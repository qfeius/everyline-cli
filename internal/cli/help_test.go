package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestHelpExplainsReviewCommandSelection(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"review", "--help"}); err != nil {
		t.Fatal(err)
	}

	help := stdout.String()
	for _, expected := range []string{
		"推荐使用 review run",
		"review file",
		"review task",
		"checklist",
		"rule",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("review help 缺少 %q: %s", expected, help)
		}
	}
}

func TestHelpExplainsReviewTaskResultIsLocalOrchestration(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"review", "task", "result", "--help"}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "自动获取并输出最终审查结果") {
		t.Fatalf("review task result help 缺少最终结果说明: %s", stdout.String())
	}
}

func TestHelpIncludesUpdateCommand(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "update") {
		t.Fatalf("根帮助缺少 update 命令: %s", stdout.String())
	}
}

func TestHelpExplainsReviewStartInputContract(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"review", "task", "start", "--help"}); err != nil {
		t.Fatal(err)
	}

	help := stdout.String()
	for _, expected := range []string{
		"businessId string（必填）",
		"fileId integer（必填）",
		"fileHash string（必填，使用上传接口返回值）",
		"config.selectedPosition string（必填）",
		"config.selectedAuditRole string（必填）",
		"config.reviewStrength string（必填，弱势|中立|强势）",
		"config.selectedCheckListIds array<string>（条件必填）",
		"config.matchContractTypeRulePackage boolean（条件必填）",
		"规则来源至少提供一项",
		"组合执行，互不覆盖且没有优先级",
		"usageReportContext.reportBusinessCode=everyLine_100_openApi_cli",
		`selectedPosition":"xxx公司"`,
		`selectedAuditRole":"xxx公司"`,
		`selectedCheckListIds":["2001001"]`,
		`matchContractTypeRulePackage":true`,
		"--data '{",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("review task start 帮助缺少 %q: %s", expected, help)
		}
	}
	for _, unexpected := range []string{"appType", "triggerScene", "reviewRules", "auditerName"} {
		if strings.Contains(help, unexpected) {
			t.Fatalf("review task start 帮助不应出现 %q: %s", unexpected, help)
		}
	}
}

func TestHelpExplainsReviewRunInputContract(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"review", "run", "--help"}); err != nil {
		t.Fatal(err)
	}

	help := stdout.String()
	for _, expected := range []string{
		"source object（必填）",
		"config object（必填）",
		"config.selectedPosition",
		"config.selectedAuditRole",
		"config.reviewStrength",
		"config.selectedCheckListIds array<string>（条件必填）",
		"config.matchContractTypeRulePackage boolean（条件必填）",
		"规则来源至少提供一项",
		"组合执行，互不覆盖且没有优先级",
		"usageReportContext.reportBusinessCode=everyLine_100_openApi_cli",
		`selectedPosition":"xxx公司"`,
		`selectedAuditRole":"xxx公司"`,
		`selectedCheckListIds":["2001001"]`,
		`matchContractTypeRulePackage":true`,
		"fileHash 使用上传接口返回值",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("review run 帮助缺少 %q: %s", expected, help)
		}
	}
	if strings.Contains(help, "appType") {
		t.Fatalf("review run 帮助不应出现 appType: %s", help)
	}
}

// TestReviewHelpDoesNotExposeAppTypeFlag 验证 app-type 已从所有相关命令的 CLI 参数中移除。
func TestReviewHelpDoesNotExposeAppTypeFlag(t *testing.T) {
	tests := [][]string{
		{"review", "file", "upload", "--help"},
		{"review", "subject", "extract", "--help"},
		{"review", "task", "status", "--help"},
		{"review", "task", "info", "--help"},
		{"review", "task", "result", "--help"},
	}

	for _, args := range tests {
		runtime, stdout, _ := testRuntime(t)
		if err := Execute(context.Background(), runtime, args); err != nil {
			t.Fatalf("args=%v err=%v", args, err)
		}
		if strings.Contains(stdout.String(), "app-type") {
			t.Fatalf("args=%v 帮助不应出现 app-type: %s", args, stdout.String())
		}
	}
}

// TestReviewFileUploadHelpDoesNotExposeBusinessIDFlag 验证本地上传命令不再暴露无效的业务对象参数。
func TestReviewFileUploadHelpDoesNotExposeBusinessIDFlag(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	if err := Execute(context.Background(), runtime, []string{"review", "file", "upload", "--help"}); err != nil {
		t.Fatal(err)
	}

	help := stdout.String()
	if strings.Contains(help, "--business-id") {
		t.Fatalf("review file upload 帮助不应出现 business-id: %s", help)
	}
	for _, expected := range []string{"--file", "--name", "--dry-run", "--print-input"} {
		if !strings.Contains(help, expected) {
			t.Fatalf("review file upload 帮助缺少 %q: %s", expected, help)
		}
	}
}

// TestReviewBusinessIDFlagsKeepContractBoundaries 验证仍有契约依据的命令继续保留 business-id。
func TestReviewBusinessIDFlagsKeepContractBoundaries(t *testing.T) {
	tests := []struct {
		args            []string
		shouldBePresent bool
	}{
		{args: []string{"review", "file", "upload", "--help"}, shouldBePresent: false},
		{args: []string{"review", "subject", "extract", "--help"}, shouldBePresent: true},
		{args: []string{"review", "task", "status", "--help"}, shouldBePresent: true},
		{args: []string{"review", "task", "info", "--help"}, shouldBePresent: true},
		{args: []string{"review", "task", "result", "--help"}, shouldBePresent: true},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatalf("args=%v err=%v", test.args, err)
			}
			present := strings.Contains(stdout.String(), "--business-id")
			if present != test.shouldBePresent {
				t.Fatalf("args=%v business-id present=%v, help=%s", test.args, present, stdout.String())
			}
		})
	}
}

// TestCommandReferenceDoesNotExposeAppTypeFlag 验证命令参考中的 CLI 语法与参数实现保持一致。
func TestCommandReferenceDoesNotExposeAppTypeFlag(t *testing.T) {
	content, err := os.ReadFile("../../docs/command-reference.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, legacySyntax := range []string{
		"review file upload --file --name [--app-type]",
		"review file upload --file --name [--business-id] [--dry-run|--print-input]",
		"review subject extract --business-id --app-type --file-id",
		"review task status --task-id [--business-id] [--app-type]",
		"review task info --task-id [--business-id] [--app-type]",
		"review task result --task-id [--business-id] [--app-type]",
	} {
		if strings.Contains(text, legacySyntax) {
			t.Fatalf("命令参考不应保留旧语法 %q", legacySyntax)
		}
	}
	for _, expectedSyntax := range []string{
		"review file upload --file --name [--dry-run|--print-input]",
		"checklist list [--name] [--review-stage] [--contract-category] [--start-time] [--end-time] [--enabled] [--create-employee-id] [--update-employee-id] [--sort] [--page-index] [--page-size]",
	} {
		if !strings.Contains(text, expectedSyntax) {
			t.Fatalf("命令参考缺少实际支持的语法 %q", expectedSyntax)
		}
	}
}

func TestHelpExplainsResourceBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{name: "checklist", args: []string{"checklist", "--help"}, expected: "审查清单由名称和关联审查规则组成"},
		{name: "rule", args: []string{"rule", "--help"}, expected: "规则必须属于规则分组"},
		{name: "auth use", args: []string{"auth", "use", "--help"}, expected: "会修改本地配置"},
		{name: "config use", args: []string{"config", "use", "--help"}, expected: "切换当前默认 Profile"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), test.expected) {
				t.Fatalf("help 缺少 %q: %s", test.expected, stdout.String())
			}
		})
	}
}

// TestHelpUsesWorkflowCommandOrder 验证命令帮助按用户流程、资源生命周期和风险顺序展示。
func TestHelpUsesWorkflowCommandOrder(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expected   []string
		unexpected []string
	}{
		{
			name:     "root",
			args:     []string{"--help"},
			expected: []string{"review", "checklist", "rule", "config", "auth", "completion", "version", "update", "help"},
		},
		{
			name:     "review",
			args:     []string{"review", "--help"},
			expected: []string{"run", "file", "subject", "task"},
		},
		{
			name:     "review task",
			args:     []string{"review", "task", "--help"},
			expected: []string{"start", "result", "status", "info"},
		},
		{
			name:     "review file",
			args:     []string{"review", "file", "--help"},
			expected: []string{"upload", "upload-url"},
		},
		{
			name:     "checklist",
			args:     []string{"checklist", "--help"},
			expected: []string{"list", "create", "batch-create", "update", "batch-update", "delete", "batch-delete"},
		},
		{
			name:     "rule",
			args:     []string{"rule", "--help"},
			expected: []string{"group", "list", "create", "batch-create", "update", "batch-update", "delete", "batch-delete"},
		},
		{
			name:     "rule group",
			args:     []string{"rule", "group", "--help"},
			expected: []string{"list", "create", "update", "delete"},
		},
		{
			name:     "auth",
			args:     []string{"auth", "--help"},
			expected: []string{"login", "status", "use", "logout"},
		},
		{
			name:     "config",
			args:     []string{"config", "--help"},
			expected: []string{"add", "use", "list", "show"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatal(err)
			}
			actual := helpCommandNames(stdout.String())
			if strings.Join(actual, ",") != strings.Join(test.expected, ",") {
				t.Fatalf("命令顺序=%v，期望=%v\n帮助输出：%s", actual, test.expected, stdout.String())
			}
		})
	}
}

// TestHelpListsDirectChildSyntaxOnly 验证 Available Commands 只展示当前命令的直属子命令名称。
func TestHelpListsDirectChildSyntaxOnly(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expected   []string
		unexpected []string
	}{
		{
			name: "review",
			args: []string{"review", "--help"},
			expected: []string{
				"  run [flags]",
				"  file [command] [flags]",
				"  subject [command] [flags]",
				"  task [command] [flags]",
			},
			unexpected: []string{
				"  review run [flags]",
				"  review file [command] [flags]",
				"  review subject [command] [flags]",
				"  review task [command] [flags]",
			},
		},
		{
			name: "review task",
			args: []string{"review", "task", "--help"},
			expected: []string{
				"  start [flags]",
				"  result [flags]",
				"  status [flags]",
				"  info [flags]",
			},
			unexpected: []string{
				"  review task start [flags]",
				"  review task result [flags]",
				"  review task status [flags]",
				"  review task info [flags]",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatal(err)
			}
			help := stdout.String()
			for _, expected := range test.expected {
				if !strings.Contains(help, "\n"+expected) {
					t.Fatalf("直属子命令展示缺少 %q: %s", expected, help)
				}
			}
			for _, unexpected := range test.unexpected {
				if strings.Contains(help, "\n"+unexpected) {
					t.Fatalf("命令列表不应显示完整父路径 %q: %s", unexpected, help)
				}
			}
		})
	}
}

// TestHelpGroupsTopLevelCommands 验证根帮助按职责分组，同时命令路径仍保持一级顶层结构。
func TestHelpGroupsTopLevelCommands(t *testing.T) {
	runtime, stdout, _ := testRuntime(t)
	root := NewRootCommand(runtime)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	help := stdout.String()
	for _, title := range []string{"Review", "CLI Management"} {
		if !strings.Contains(help, title) {
			t.Fatalf("根帮助缺少分组 %q: %s", title, help)
		}
	}
	if strings.Contains(help, "Environment & Identity") {
		t.Fatalf("根帮助不应继续显示独立的 Environment & Identity 分组: %s", help)
	}
	if strings.Index(help, "Review") > strings.Index(help, "CLI Management") {
		t.Fatalf("Review 分组应位于 CLI Management 之前: %s", help)
	}
	if strings.Contains(help, "Use \"everyline-cli [command] --help\"") {
		t.Fatalf("根帮助不应再显示重复的 Use 引导: %s", help)
	}
	if strings.Contains(help, "  everyline-cli review [command] [flags]") {
		t.Fatalf("命令列表不应重复显示 everyline-cli 前缀: %s", help)
	}
	flagsIndex := strings.Index(help, "Flags:")
	notesIndex := strings.Index(help, "Notes:")
	if notesIndex < flagsIndex {
		t.Fatalf("Notes 应位于 Flags 之后: %s", help)
	}
	if strings.Contains(help, "客户端。\n\n用于") || strings.Contains(help, "用于合同文件准备、智能审查任务执行、审查清单管理和审查规则管理。\n\n完整审查流程") {
		t.Fatalf("根帮助说明中不应出现多余空行: %s", help)
	}
	if strings.Contains(help, "everyline-cli domains") || strings.Contains(help, "everyline-cli management") {
		t.Fatalf("帮助不应增加中间命令层级: %s", help)
	}

	for _, name := range []string{"review", "checklist", "rule", "config", "auth"} {
		command, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("查找顶层命令 %q 失败: %v", name, err)
		}
		if command.CommandPath() != "everyline-cli "+name {
			t.Fatalf("命令 %q 被改变路径为 %q", name, command.CommandPath())
		}
	}
}

// TestHelpRendersCommandSyntaxAndNotes 验证帮助显示完整命令语法和实现约束 Notes。
func TestHelpRendersCommandSyntaxAndNotes(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expected   []string
		unexpected []string
	}{
		{
			name: "root",
			args: []string{"--help"},
			expected: []string{
				"review [command] [flags]",
				"Notes:",
				"使用 everyline-cli <command> --help 查看具体命令参数。",
			},
		},
		{
			name: "review task",
			args: []string{"review", "task", "--help"},
			expected: []string{
				"everyline-cli review task [command] [flags]",
				"result 会轮询任务状态，成功后自动获取最终审查详情。",
			},
		},
		{
			name: "config add",
			args: []string{"config", "add", "--help"},
			expected: []string{
				"everyline-cli config add <name> [flags]",
				"Profile 不保存 app secret 或 access token。",
				"user 身份可省略",
				"--oauth-metadata-url string",
				"--oauth-redirect-url string",
			},
		},
		{
			name: "auth login",
			args: []string{"auth", "login", "--help"},
			expected: []string{
				"everyline-cli auth login [flags]",
				"--app-id string",
				"--app-secret string",
				"--app-secret-stdin",
				"--save-app-secret",
				"--no-open-browser",
			},
			unexpected: []string{
				"--access-token-stdin",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.expected {
				if !strings.Contains(stdout.String(), expected) {
					t.Fatalf("帮助缺少 %q: %s", expected, stdout.String())
				}
			}
			for _, unexpected := range test.unexpected {
				if strings.Contains(stdout.String(), unexpected) {
					t.Fatalf("帮助不应包含 %q: %s", unexpected, stdout.String())
				}
			}
		})
	}
}

// TestHelpPlacesCommandFlagsBeforeHelpFlag 验证命令业务 flags 连续显示在自动 help flag 之前。
func TestHelpPlacesCommandFlagsBeforeHelpFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		business  []string
		operation []string
	}{
		{
			name:      "review file upload",
			args:      []string{"review", "file", "upload", "--help"},
			business:  []string{"--file", "--name"},
			operation: []string{"--dry-run", "--print-input"},
		},
		{
			name:      "review task start",
			args:      []string{"review", "task", "start", "--help"},
			business:  []string{"--data", "--input"},
			operation: []string{"--dry-run", "--print-input"},
		},
		{
			name:     "config add",
			args:     []string{"config", "add", "--help"},
			business: []string{"--app-id", "--base-url", "--env", "--oauth-client-id", "--token-url"},
		},
		{
			name:     "auth login",
			args:     []string{"auth", "login", "--help"},
			business: []string{"--app-id", "--app-secret", "--app-secret-stdin", "--no-open-browser", "--save-app-secret"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, stdout, _ := testRuntime(t)
			if err := Execute(context.Background(), runtime, test.args); err != nil {
				t.Fatal(err)
			}
			flags := helpFlagsSection(stdout.String())
			helpIndex := strings.Index(flags, "-h, --help")
			if helpIndex < 0 {
				t.Fatalf("Flags 区域缺少 help flag: %s", flags)
			}
			assertOrder := func(flagNames []string) {
				previous := -1
				for _, flagName := range flagNames {
					current := strings.Index(flags, flagName)
					if current < 0 {
						t.Fatalf("Flags 区域缺少 %q: %s", flagName, flags)
					}
					if current > helpIndex {
						t.Fatalf("业务 flag %q 出现在 help 之后: %s", flagName, flags)
					}
					if current < previous {
						t.Fatalf("同组 flags 未保持字母序: %s", flags)
					}
					previous = current
				}
			}
			assertOrder(test.business)
			assertOrder(test.operation)
			if len(test.business) > 0 && len(test.operation) > 0 && strings.Index(flags, test.business[len(test.business)-1]) > strings.Index(flags, test.operation[0]) {
				t.Fatalf("通用操作 flags 不应出现在业务 flags 之前: %s", flags)
			}
			if globalIndex := strings.Index(stdout.String(), "Global Flags:"); globalIndex < strings.Index(stdout.String(), "Flags:") {
				t.Fatalf("Global Flags 不应出现在 Flags 之前: %s", stdout.String())
			}
		})
	}
}

func helpFlagsSection(help string) string {
	start := strings.Index(help, "Flags:\n")
	if start < 0 {
		return ""
	}
	start += len("Flags:\n")
	end := strings.Index(help[start:], "\n\nGlobal Flags:")
	if end < 0 {
		return help[start:]
	}
	return help[start : start+end]
}

func helpCommandNames(help string) []string {
	lines := strings.Split(help, "\n")
	commands := make([]string, 0)
	knownCommands := map[string]bool{
		"review": true, "checklist": true, "rule": true, "config": true, "auth": true,
		"completion": true, "version": true, "help": true,
		"run": true, "file": true, "subject": true, "task": true,
		"start": true, "result": true, "status": true, "info": true,
		"upload": true, "upload-url": true, "extract": true,
		"list": true, "create": true, "batch-create": true, "update": true,
		"batch-update": true, "delete": true, "batch-delete": true,
		"group": true, "login": true, "use": true, "logout": true,
		"add": true, "show": true,
	}
	knownGroupTitles := map[string]bool{"Review": true, "CLI Management": true}
	inCommands := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Available Commands:" {
			inCommands = true
			continue
		}
		if knownGroupTitles[trimmed] {
			inCommands = true
			continue
		}
		if inCommands && (trimmed == "Flags:" || trimmed == "Global Flags:") {
			break
		}
		if !inCommands || trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		start := 0
		if fields[0] == "everyline-cli" {
			start = 1
		}
		if start >= len(fields) || !knownCommands[fields[start]] {
			continue
		}
		commandName := ""
		for _, field := range fields[start:] {
			if strings.HasPrefix(field, "[") || strings.HasPrefix(field, "<") {
				break
			}
			commandName = field
		}
		if knownCommands[commandName] {
			commands = append(commands, commandName)
		}
	}
	return commands
}
