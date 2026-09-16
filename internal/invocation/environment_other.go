//go:build !windows

package invocation

func platformEnvironmentFallback(result Result) Result { return result }
