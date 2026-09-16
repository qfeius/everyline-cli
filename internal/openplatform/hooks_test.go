package openplatform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
)

func testProfile(baseURL string) config.Profile { return config.Profile{BaseURL: baseURL} }

func hookTestRequest() Request {
	return Request{OperationID: "listReviewChecklists", Method: http.MethodGet,
		Path: "/open-apis/review-rules/review-checklists", ContractInput: map[string]any{}}
}

func TestBeforeRequestHooksRunInOrderForEveryAttempt(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := calls.Add(1)
		if r.Header.Get("X-Test-Attempt") != fmt.Sprint(attempt) || r.Header.Get("X-Test-Order") != "first,second" {
			t.Errorf("stale or missing hook values: %v", r.Header)
		}
		if r.Header.Get("Authorization") != "Bearer token-test" || r.Header.Get("User-Agent") != "everyline-cli" {
			t.Error("hook changed authentication or user agent")
		}
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"code":503,"msg":"retry","data":null}`)
			return
		}
		fmt.Fprint(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	var inspections int
	hooks := []BeforeRequestHook{
		func(_ context.Context, r *http.Request) error {
			inspections++
			r.Header.Set("X-Test-Attempt", fmt.Sprint(inspections))
			r.Header.Set("X-Test-Order", "first")
			return nil
		},
		func(_ context.Context, r *http.Request) error {
			r.Header.Set("X-Test-Order", r.Header.Get("X-Test-Order")+",second")
			return nil
		},
	}
	option := WithBeforeRequestHooks(hooks...)
	hooks[0] = func(context.Context, *http.Request) error { return errors.New("mutated registration") }
	client := NewClient(testProfile(server.URL), staticTokenProvider{}, server.Client(), option)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	operation := hookTestRequest()
	operation.Header = http.Header{"X-Test-Attempt": {"old"}}
	before := operation.Header.Clone()
	if _, err := client.Do(context.Background(), operation); err != nil {
		t.Fatal(err)
	}
	if inspections != 2 || calls.Load() != 2 {
		t.Fatalf("inspections=%d requests=%d", inspections, calls.Load())
	}
	if !reflect.DeepEqual(operation.Header, before) {
		t.Fatalf("caller header map mutated: %v", operation.Header)
	}
}

func TestBeforeRequestHooksDoNotSendOrRetryOnCancellationOrHookError(t *testing.T) {
	for _, mode := range []string{"cancel-before", "cancel-in-hook", "hook-error", "deadline-in-hook"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, `{"code":200,"data":[]}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel-before" {
				cancel()
			}
			failure := errors.New("hook failure")
			inspections := 0
			client := NewClient(testProfile(server.URL), staticTokenProvider{}, server.Client(), WithBeforeRequestHooks(
				func(ctx context.Context, _ *http.Request) error {
					inspections++
					switch mode {
					case "cancel-in-hook":
						cancel()
					case "hook-error":
						return failure
					case "deadline-in-hook":
						<-ctx.Done()
					}
					return nil
				}))
			operation := hookTestRequest()
			operation.Timeout = 20 * time.Millisecond
			_, err := client.Do(ctx, operation)
			want := error(context.Canceled)
			if mode == "hook-error" {
				want = failure
			} else if mode == "deadline-in-hook" {
				want = context.DeadlineExceeded
			}
			if !errors.Is(err, want) || calls.Load() != 0 || inspections > 1 {
				t.Fatalf("err=%v requests=%d inspections=%d", err, calls.Load(), inspections)
			}
			if mode == "cancel-before" && inspections != 0 {
				t.Fatal("cancelled request still inspected")
			}
		})
	}
}
