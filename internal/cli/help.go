package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const everylineNotesAnnotation = "everyline.notes"

// everylineHelpTemplate 保留 Cobra 默认帮助结构，并将命令条目和 Notes 调整为 CLI 约定格式。
const everylineHelpTemplate = `{{with everylineDescription .}}{{.}}

{{end}}Usage:
  {{everylineCommandSyntax .}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad (everylineCommandListSyntax .) 48}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad (everylineCommandListSyntax .) 48}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad (everylineCommandListSyntax .) 48}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{everylineLocalFlagUsages .LocalFlags | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{with index .Annotations "everyline.notes"}}

Notes:
{{.}}{{end}}
`

func init() {
	cobra.AddTemplateFunc("everylineCommandSyntax", everylineCommandSyntax)
	cobra.AddTemplateFunc("everylineCommandListSyntax", everylineCommandListSyntax)
	cobra.AddTemplateFunc("everylineDescription", everylineDescription)
	cobra.AddTemplateFunc("everylineLocalFlagUsages", everylineLocalFlagUsages)
}

// everylineLocalFlagUsages 将命令业务 flags 排在自动 help flag 之前，并保持业务 flags 的字母序。
func everylineLocalFlagUsages(flags *pflag.FlagSet) string {
	if flags == nil {
		return ""
	}
	businessFlags := make([]*pflag.Flag, 0)
	operationFlags := make([]*pflag.Flag, 0)
	var helpFlag *pflag.Flag
	flags.VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "help" {
			helpFlag = flag
			return
		}
		if flag.Name == "dry-run" || flag.Name == "print-input" {
			operationFlags = append(operationFlags, flag)
			return
		}
		businessFlags = append(businessFlags, flag)
	})
	sortFlags := func(flags []*pflag.Flag) {
		sort.SliceStable(flags, func(left, right int) bool {
			return flags[left].Name < flags[right].Name
		})
	}
	sortFlags(businessFlags)
	sortFlags(operationFlags)
	ordered := make([]*pflag.Flag, 0, len(businessFlags)+len(operationFlags)+1)
	ordered = append(ordered, businessFlags...)
	ordered = append(ordered, operationFlags...)
	if helpFlag != nil {
		ordered = append(ordered, helpFlag)
	}

	orderedFlags := pflag.NewFlagSet(flags.Name(), pflag.ContinueOnError)
	orderedFlags.SortFlags = false
	for _, flag := range ordered {
		orderedFlags.AddFlag(flag)
	}
	return orderedFlags.FlagUsages()
}

// everylineDescription 压缩命令说明中的空行，保留非空行的语义顺序。
func everylineDescription(command *cobra.Command) string {
	description := strings.TrimSpace(command.Long)
	if description == "" {
		description = strings.TrimSpace(command.Short)
	}
	lines := strings.Split(description, "\n")
	compact := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			compact = append(compact, line)
		}
	}
	return strings.Join(compact, "\n")
}

// everylineCommandSyntax 构造帮助列表和 Usage 中的完整命令语法。
func everylineCommandSyntax(command *cobra.Command) string {
	syntax := strings.TrimSpace(command.UseLine())
	syntax = strings.TrimSuffix(syntax, " [flags]")
	if command.HasAvailableSubCommands() && !strings.Contains(syntax, "[command]") {
		syntax += " [command]"
	}
	syntax += " [flags]"
	return syntax
}

// everylineCommandListSyntax 构造命令列表中的直属子命令语法，避免重复显示父命令路径。
func everylineCommandListSyntax(command *cobra.Command) string {
	syntax := everylineCommandSyntax(command)
	prefix := command.Root().Name()
	if parent := command.Parent(); parent != nil {
		prefix = parent.CommandPath()
	}
	return strings.TrimSpace(strings.TrimPrefix(syntax, prefix+" "))
}

// withNotes 为命令写入基于实际实现的使用约束，供自定义帮助模板渲染。
func withNotes(command *cobra.Command, notes ...string) *cobra.Command {
	lines := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		lines = append(lines, "  - "+note)
	}
	if len(lines) == 0 {
		return command
	}
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[everylineNotesAnnotation] = strings.Join(lines, "\n")
	return command
}
