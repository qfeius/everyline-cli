package cli

import (
	"context"
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
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "root",
			args:     []string{"--help"},
			expected: []string{"review", "checklist", "rule", "config", "auth", "completion", "version", "help"},
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
		name     string
		args     []string
		expected []string
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
		})
	}
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
