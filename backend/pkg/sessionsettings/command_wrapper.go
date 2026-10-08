package sessionsettings

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"
)

const MaxCommandWrapperTemplateSize = 64 * 1024

type commandWrapperData struct {
	Command string
}

// ValidateCommandWrapperTemplate validates the intentionally small template
// language used by session profiles. Only literal text and one {{ .Command }}
// action are accepted; control flow and function calls are deliberately not
// part of the public contract.
func ValidateCommandWrapperTemplate(source string) error {
	if source == "" {
		return nil
	}
	if len(source) > MaxCommandWrapperTemplateSize {
		return fmt.Errorf("command wrapper template exceeds %d bytes", MaxCommandWrapperTemplateSize)
	}
	if strings.IndexByte(source, 0) >= 0 {
		return fmt.Errorf("command wrapper template contains a NUL byte")
	}
	tmpl, err := template.New("command-wrapper").Option("missingkey=error").Parse(source)
	if err != nil {
		return fmt.Errorf("invalid command wrapper template: %w", err)
	}
	commandActions := 0
	for _, node := range tmpl.Tree.Root.Nodes {
		switch typed := node.(type) {
		case *parse.TextNode, *parse.CommentNode:
			continue
		case *parse.ActionNode:
			if !isCommandAction(typed) {
				return fmt.Errorf("command wrapper template only supports the {{ .Command }} action")
			}
			commandActions++
		default:
			return fmt.Errorf("command wrapper template only supports literal text and the {{ .Command }} action")
		}
	}
	if commandActions != 1 {
		return fmt.Errorf("command wrapper template must contain exactly one {{ .Command }} action")
	}
	return nil
}

func isCommandAction(action *parse.ActionNode) bool {
	pipe := action.Pipe
	if pipe == nil || len(pipe.Decl) != 0 || pipe.IsAssign || len(pipe.Cmds) != 1 {
		return false
	}
	command := pipe.Cmds[0]
	if command == nil || len(command.Args) != 1 {
		return false
	}
	field, ok := command.Args[0].(*parse.FieldNode)
	return ok && len(field.Ident) == 1 && field.Ident[0] == "Command"
}

// RenderCommandWrapper inserts a safely shell-quoted argv into a validated
// wrapper template. The returned script is suitable for /bin/sh -c.
func RenderCommandWrapper(source string, argv []string) (string, error) {
	if source == "" {
		return "", nil
	}
	if err := ValidateCommandWrapperTemplate(source); err != nil {
		return "", err
	}
	tmpl, err := template.New("command-wrapper").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse command wrapper template: %w", err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, commandWrapperData{Command: ShellJoin(argv)}); err != nil {
		return "", fmt.Errorf("render command wrapper template: %w", err)
	}
	if rendered.Len() > MaxCommandWrapperTemplateSize {
		return "", fmt.Errorf("rendered command wrapper exceeds %d bytes", MaxCommandWrapperTemplateSize)
	}
	return rendered.String(), nil
}

// ShellJoin converts argv to a POSIX-shell-safe command string without
// allowing shell expansion in any individual argument.
func ShellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return strings.Join(quoted, " ")
}
