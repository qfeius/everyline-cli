package filelock

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// With 在回调执行期间持有跨进程文件锁，防止多个 CLI 进程覆盖同一份文件快照。
// 入参：path string 为锁文件路径；action func() error 为锁内执行的读改写操作。
// 返回值：error，创建目录、加锁、执行回调或解锁失败时非 nil。
func With(path string, action func() error) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建锁文件目录: %w", err)
	}
	lock := flock.New(path)
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("获取文件锁: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("释放文件锁: %w", err)
		}
	}()
	return action()
}
