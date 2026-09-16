package cli

import (
	"context"
	"fmt"
	"net/http"

	"git.qtech.cn/ai/everyline-cli/internal/config"
	"git.qtech.cn/ai/everyline-cli/internal/invocation"
	"git.qtech.cn/ai/everyline-cli/internal/openplatform"
)

// newOpenPlatformClient 集中装配业务客户端，保证 review/checklist/rule 使用同一套钩子。
// 鉴权 Provider 不挂业务钩子；不修改 token、Profile 或业务请求体。
func (runtime *Runtime) newOpenPlatformClient(profile config.Profile, identity config.IdentityKind, verbose bool) *openplatform.Client {
	provider := newTokenProvider(runtime)
	options := []openplatform.ClientOption{openplatform.WithBeforeRequestHooks(runtime.environmentHook(verbose))}
	if verbose {
		options = append(options, openplatform.WithTraceOutput(runtime.Error))
	}
	return openplatform.NewClientForIdentity(profile, provider, runtime.HTTP, identity, options...)
}

func (runtime *Runtime) environmentHook(verbose bool) openplatform.BeforeRequestHook {
	return func(ctx context.Context, request *http.Request) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		inspect := runtime.InspectEnvironment
		if inspect == nil {
			inspect = invocation.Inspect
		}
		report := inspect(ctx, invocation.DefaultMaxDepth)
		// 探测自己的 5 秒超时可降级，原业务请求的取消/截止时间仍必须遵守。
		if err := ctx.Err(); err != nil {
			return err
		}
		invocation.ApplyHeaders(request.Header, report)
		if verbose && runtime.Error != nil {
			// 仅输出实际附加的白名单字段，不输出 token、完整进程路径或合同信息。
			_, _ = fmt.Fprintf(runtime.Error,
				"invocation_source channel_type=%q agent_source_type=%q product_code=%q evidence_type=%q confidence=%q detector_version=%q rule_id=%q\n",
				request.Header.Get(invocation.HeaderChannelType), request.Header.Get(invocation.HeaderAgentSourceType),
				request.Header.Get(invocation.HeaderProductCode), request.Header.Get(invocation.HeaderEvidenceType),
				request.Header.Get(invocation.HeaderConfidence), request.Header.Get(invocation.HeaderDetectorVersion),
				request.Header.Get(invocation.HeaderRuleID))
		}
		return nil
	}
}
