//go:build windows

package invocation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	appModelErrorNoPackage = windows.Errno(15700)
	cmsgSignerInfoParam    = 6
)

var (
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	getPackageFamilyNameProcess = kernel32.NewProc("GetPackageFamilyName")
	crypt32                     = windows.NewLazySystemDLL("crypt32.dll")
	cryptMsgGetParam            = crypt32.NewProc("CryptMsgGetParam")
	cryptMsgClose               = crypt32.NewProc("CryptMsgClose")
)

type cryptAttributes struct {
	Count      uint32
	Attributes *cryptAttribute
}

type cryptAttribute struct {
	ObjectID   *byte
	ValueCount uint32
	Values     *windows.CryptAttrBlob
}

type cmsgSignerInfo struct {
	Version                 uint32
	Issuer                  windows.CertNameBlob
	SerialNumber            windows.CryptIntegerBlob
	HashAlgorithm           windows.CryptAlgorithmIdentifier
	HashEncryptionAlgorithm windows.CryptAlgorithmIdentifier
	EncryptedHash           windows.CryptDataBlob
	AuthenticatedAttributes cryptAttributes
	UnauthenticatedAttrs    cryptAttributes
}

func platformApplicationIdentities(chain []Process) ([]ApplicationIdentity, []string) {
	if len(chain) < 2 {
		return nil, nil
	}

	identities := make([]ApplicationIdentity, 0)
	warnings := make([]string, 0)
	for _, current := range chain[1:] {
		packageFamilyName, packageErr := processPackageFamilyName(current.PID)
		rule := matchingWindowsRule(current)
		if packageErr != nil && rule != nil {
			warnings = append(warnings, fmt.Sprintf("cannot read Windows package identity for pid %d: %v", current.PID, packageErr))
		}

		identity := ApplicationIdentity{
			ProcessDepth:      current.Depth,
			ExecutablePath:    current.Executable,
			PackageFamilyName: packageFamilyName,
		}
		if packageFamilyName != "" {
			identities = append(identities, identity)
			continue
		}
		if (rule == nil && !windowsFeishuExecutable(current.Executable)) || strings.TrimSpace(current.Executable) == "" {
			continue
		}

		publisher, certificateSHA256, err := inspectAuthenticode(current.Executable)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot verify Authenticode signature for %s: %v", current.Executable, err))
			identities = append(identities, identity)
			continue
		}
		identity.Publisher = publisher
		identity.CertificateSHA256 = certificateSHA256
		identity.SignatureValid = true
		identities = append(identities, identity)
	}
	return identities, warnings
}

func processPackageFamilyName(pid int32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	var length uint32
	status, _, _ := getPackageFamilyNameProcess.Call(uintptr(handle), uintptr(unsafe.Pointer(&length)), 0)
	switch windows.Errno(status) {
	case windows.ERROR_INSUFFICIENT_BUFFER:
	case appModelErrorNoPackage:
		return "", nil
	case windows.ERROR_SUCCESS:
		if length == 0 {
			return "", nil
		}
	default:
		return "", windows.Errno(status)
	}
	if length == 0 {
		return "", nil
	}

	buffer := make([]uint16, length)
	status, _, _ = getPackageFamilyNameProcess.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&length)),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if windows.Errno(status) != windows.ERROR_SUCCESS {
		return "", windows.Errno(status)
	}
	return windows.UTF16ToString(buffer), nil
}

func inspectAuthenticode(executable string) (string, string, error) {
	path, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return "", "", err
	}
	fileInfo := windows.WinTrustFileInfo{
		Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
		FilePath: path,
	}
	trustData := windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&fileInfo),
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		ProvFlags:                       windows.WTD_REVOCATION_CHECK_NONE | windows.WTD_CACHE_ONLY_URL_RETRIEVAL,
		UIContext:                       windows.WTD_UICONTEXT_EXECUTE,
	}

	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &trustData)
	trustData.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &trustData)
	runtime.KeepAlive(path)
	if verifyErr != nil {
		return "", "", verifyErr
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("close WinVerifyTrust state: %w", closeErr)
	}

	publisher, fingerprint, err := authenticodeSigner(path)
	if err != nil {
		return "", "", err
	}
	return publisher, fingerprint, nil
}

func authenticodeSigner(path *uint16) (string, string, error) {
	var encodingType uint32
	var contentType uint32
	var formatType uint32
	var store windows.Handle
	var message windows.Handle
	var context unsafe.Pointer
	err := windows.CryptQueryObject(
		windows.CERT_QUERY_OBJECT_FILE,
		unsafe.Pointer(path),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED,
		windows.CERT_QUERY_FORMAT_FLAG_BINARY,
		0,
		&encodingType,
		&contentType,
		&formatType,
		&store,
		&message,
		&context,
	)
	if err != nil {
		return "", "", fmt.Errorf("read embedded signature: %w", err)
	}
	defer windows.CertCloseStore(store, 0)
	defer closeCryptMessage(message)

	var signerSize uint32
	if err := callCryptMsgGetParam(message, cmsgSignerInfoParam, 0, nil, &signerSize); err != nil {
		return "", "", fmt.Errorf("size signer information: %w", err)
	}
	if signerSize < uint32(unsafe.Sizeof(cmsgSignerInfo{})) {
		return "", "", errors.New("embedded signature did not contain signer information")
	}
	signerBuffer := make([]byte, signerSize)
	if err := callCryptMsgGetParam(message, cmsgSignerInfoParam, 0, unsafe.Pointer(&signerBuffer[0]), &signerSize); err != nil {
		return "", "", fmt.Errorf("read signer information: %w", err)
	}
	signer := (*cmsgSignerInfo)(unsafe.Pointer(&signerBuffer[0]))
	certificateInfo := windows.CertInfo{
		Issuer:       signer.Issuer,
		SerialNumber: signer.SerialNumber,
	}
	certificate, err := windows.CertFindCertificateInStore(
		store,
		windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING,
		0,
		windows.CERT_FIND_SUBJECT_CERT,
		unsafe.Pointer(&certificateInfo),
		nil,
	)
	if err != nil {
		return "", "", fmt.Errorf("find signer certificate: %w", err)
	}
	defer windows.CertFreeCertificateContext(certificate)
	if certificate.EncodedCert == nil || certificate.Length == 0 {
		return "", "", errors.New("signer certificate was empty")
	}

	encoded := unsafe.Slice(certificate.EncodedCert, certificate.Length)
	digest := sha256.Sum256(encoded)
	return certificateDisplayName(certificate), hex.EncodeToString(digest[:]), nil
}

func callCryptMsgGetParam(message windows.Handle, paramType, index uint32, data unsafe.Pointer, size *uint32) error {
	result, _, callErr := cryptMsgGetParam.Call(
		uintptr(message),
		uintptr(paramType),
		uintptr(index),
		uintptr(data),
		uintptr(unsafe.Pointer(size)),
	)
	if result != 0 {
		return nil
	}
	if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return callErr
	}
	return errors.New("CryptMsgGetParam failed")
}

func closeCryptMessage(message windows.Handle) {
	if message != 0 {
		cryptMsgClose.Call(uintptr(message))
	}
}

func certificateDisplayName(certificate *windows.CertContext) string {
	length := windows.CertGetNameString(certificate, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, nil, 0)
	if length <= 1 {
		return ""
	}
	buffer := make([]uint16, length)
	windows.CertGetNameString(certificate, windows.CERT_NAME_SIMPLE_DISPLAY_TYPE, 0, nil, &buffer[0], length)
	return windows.UTF16ToString(buffer)
}
