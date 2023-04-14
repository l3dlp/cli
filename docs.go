//go:build !urfave_cli_no_docs
// +build !urfave_cli_no_docs

package cli

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/cpuguy83/go-md2man/v2/md2man"
)

type (
	tabularOptions struct {
		appPath string
	}

	TabularOption func(*tabularOptions)
)

// WithTabularAppPath allows to override the default app path.
func WithTabularAppPath(path string) TabularOption {
	return func(o *tabularOptions) { o.appPath = path }
}

// ToTabularMarkdown creates a tabular markdown documentation for the `*App`.
// The function errors if either parsing or writing of the string fails.
func (a *App) ToTabularMarkdown(opts ...TabularOption) (string, error) {
	var o = tabularOptions{
		appPath: "app",
	}

	for _, opt := range opts {
		opt(&o)
	}

	const name = "cli"

	t, err := template.New(name).Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(MarkdownTabularDocTemplate)
	if err != nil {
		return "", err
	}

	var (
		w  bytes.Buffer
		tt tabularTemplate
	)

	if err = t.ExecuteTemplate(&w, name, cliTabularAppTemplate{
		AppPath:     o.appPath,
		Name:        a.Name,
		Description: tt.PrepareMultilineString(a.Description),
		Usage:       tt.PrepareMultilineString(a.Usage),
		UsageText:   strings.FieldsFunc(a.UsageText, func(r rune) bool { return r == '\n' }),
		ArgsUsage:   tt.PrepareMultilineString(a.ArgsUsage),
		GlobalFlags: tt.PrepareFlags(a.VisibleFlags()),
		Commands:    tt.PrepareCommands(a.VisibleCommands(), o.appPath, "", 0),
	}); err != nil {
		return "", err
	}

	return tt.Prettify(w.String()), nil
}

// ToMarkdown creates a markdown string for the `*App`
// The function errors if either parsing or writing of the string fails.
func (a *App) ToMarkdown() (string, error) {
	var w bytes.Buffer
	if err := a.writeDocTemplate(&w, 0); err != nil {
		return "", err
	}
	return w.String(), nil
}

// ToMan creates a man page string with section number for the `*App`
// The function errors if either parsing or writing of the string fails.
func (a *App) ToManWithSection(sectionNumber int) (string, error) {
	var w bytes.Buffer
	if err := a.writeDocTemplate(&w, sectionNumber); err != nil {
		return "", err
	}
	man := md2man.Render(w.Bytes())
	return string(man), nil
}

// ToMan creates a man page string for the `*App`
// The function errors if either parsing or writing of the string fails.
func (a *App) ToMan() (string, error) {
	man, err := a.ToManWithSection(8)
	return man, err
}

type cliTemplate struct {
	App          *App
	SectionNum   int
	Commands     []string
	GlobalArgs   []string
	SynopsisArgs []string
}

func (a *App) writeDocTemplate(w io.Writer, sectionNum int) error {
	const name = "cli"
	t, err := template.New(name).Parse(MarkdownDocTemplate)
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, name, &cliTemplate{
		App:          a,
		SectionNum:   sectionNum,
		Commands:     prepareCommands(a.Commands, 0),
		GlobalArgs:   prepareArgsWithValues(a.VisibleFlags()),
		SynopsisArgs: prepareArgsSynopsis(a.VisibleFlags()),
	})
}

func prepareCommands(commands []*Command, level int) []string {
	var coms []string
	for _, command := range commands {
		if command.Hidden {
			continue
		}

		usageText := prepareUsageText(command)

		usage := prepareUsage(command, usageText)

		prepared := fmt.Sprintf("%s %s\n\n%s%s",
			strings.Repeat("#", level+2),
			strings.Join(command.Names(), ", "),
			usage,
			usageText,
		)

		flags := prepareArgsWithValues(command.VisibleFlags())
		if len(flags) > 0 {
			prepared += fmt.Sprintf("\n%s", strings.Join(flags, "\n"))
		}

		coms = append(coms, prepared)

		// recursively iterate subcommands
		if len(command.Commands) > 0 {
			coms = append(
				coms,
				prepareCommands(command.Commands, level+1)...,
			)
		}
	}

	return coms
}

func prepareArgsWithValues(flags []Flag) []string {
	return prepareFlags(flags, ", ", "**", "**", `""`, true)
}

func prepareArgsSynopsis(flags []Flag) []string {
	return prepareFlags(flags, "|", "[", "]", "[value]", false)
}

func prepareFlags(
	flags []Flag,
	sep, opener, closer, value string,
	addDetails bool,
) []string {
	args := []string{}
	for _, f := range flags {
		flag, ok := f.(DocGenerationFlag)
		if !ok {
			continue
		}
		modifiedArg := opener

		for _, s := range flag.Names() {
			trimmed := strings.TrimSpace(s)
			if len(modifiedArg) > len(opener) {
				modifiedArg += sep
			}
			if len(trimmed) > 1 {
				modifiedArg += fmt.Sprintf("--%s", trimmed)
			} else {
				modifiedArg += fmt.Sprintf("-%s", trimmed)
			}
		}
		modifiedArg += closer
		if flag.TakesValue() {
			modifiedArg += fmt.Sprintf("=%s", value)
		}

		if addDetails {
			modifiedArg += flagDetails(flag)
		}

		args = append(args, modifiedArg+"\n")

	}
	sort.Strings(args)
	return args
}

// flagDetails returns a string containing the flags metadata
func flagDetails(flag DocGenerationFlag) string {
	description := flag.GetUsage()
	value := flag.GetValue()
	if value != "" {
		description += " (default: " + value + ")"
	}
	return ": " + description
}

func prepareUsageText(command *Command) string {
	if command.UsageText == "" {
		return ""
	}

	// Remove leading and trailing newlines
	preparedUsageText := strings.Trim(command.UsageText, "\n")

	var usageText string
	if strings.Contains(preparedUsageText, "\n") {
		// Format multi-line string as a code block using the 4 space schema to allow for embedded markdown such
		// that it will not break the continuous code block.
		for _, ln := range strings.Split(preparedUsageText, "\n") {
			usageText += fmt.Sprintf("    %s\n", ln)
		}
	} else {
		// Style a single line as a note
		usageText = fmt.Sprintf(">%s\n", preparedUsageText)
	}

	return usageText
}

func prepareUsage(command *Command, usageText string) string {
	if command.Usage == "" {
		return ""
	}

	usage := command.Usage + "\n"
	// Add a newline to the Usage IFF there is a UsageText
	if usageText != "" {
		usage += "\n"
	}

	return usage
}

type (
	cliTabularAppTemplate struct {
		AppPath          string
		Name             string
		Usage, ArgsUsage string
		UsageText        []string
		Description      string
		GlobalFlags      []cliTabularFlagTemplate
		Commands         []cliTabularCommandTemplate
	}

	cliTabularCommandTemplate struct {
		AppPath          string
		Name             string
		Aliases          []string
		Usage, ArgsUsage string
		UsageText        []string
		Description      string
		Category         string
		Flags            []cliTabularFlagTemplate
		SubCommands      []cliTabularCommandTemplate
		Level            uint
	}

	cliTabularFlagTemplate struct {
		Name       string
		Aliases    []string
		Usage      string
		TakesValue bool
		Default    string
		EnvVars    []string
	}
)

// tabularTemplate is a struct for the tabular template preparation.
type tabularTemplate struct{}

// PrepareCommands converts CLI commands into a structs for the rendering.
func (tt tabularTemplate) PrepareCommands(commands []*Command, appPath, parentCommandName string, level uint) []cliTabularCommandTemplate {
	var result = make([]cliTabularCommandTemplate, 0, len(commands))

	for _, cmd := range commands {
		var command = cliTabularCommandTemplate{
			AppPath:     appPath,
			Name:        strings.TrimSpace(strings.Join([]string{parentCommandName, cmd.Name}, " ")),
			Aliases:     cmd.Aliases,
			Usage:       tt.PrepareMultilineString(cmd.Usage),
			UsageText:   strings.FieldsFunc(cmd.UsageText, func(r rune) bool { return r == '\n' }),
			ArgsUsage:   tt.PrepareMultilineString(cmd.ArgsUsage),
			Description: tt.PrepareMultilineString(cmd.Description),
			Category:    cmd.Category,
			Flags:       tt.PrepareFlags(cmd.VisibleFlags()),
			SubCommands: tt.PrepareCommands( // note: recursive call
				cmd.Commands,
				appPath,
				strings.Join([]string{parentCommandName, cmd.Name}, " "),
				level+1,
			),
			Level: level,
		}

		result = append(result, command)
	}

	return result
}

// PrepareFlags converts CLI flags into a structs for the rendering.
func (tt tabularTemplate) PrepareFlags(flags []Flag) []cliTabularFlagTemplate {
	var result = make([]cliTabularFlagTemplate, 0, len(flags))

	for _, appFlag := range flags {
		flag, ok := appFlag.(DocGenerationFlag)
		if !ok {
			continue
		}

		var f = cliTabularFlagTemplate{
			Usage:      tt.PrepareMultilineString(flag.GetUsage()),
			EnvVars:    flag.GetEnvVars(),
			TakesValue: flag.TakesValue(),
			Default:    flag.GetValue(),
		}

		if boolFlag, isBool := appFlag.(*BoolFlag); isBool {
			f.Default = strconv.FormatBool(boolFlag.Value)
		}

		for i, name := range flag.Names() {
			name = strings.TrimSpace(name)

			if i == 0 {
				f.Name = "--" + name

				continue
			}

			if len(name) > 1 {
				name = "--" + name
			} else {
				name = "-" + name
			}

			f.Aliases = append(f.Aliases, name)
		}

		result = append(result, f)
	}

	return result
}

// PrepareMultilineString prepares a string (removes line breaks).
func (tabularTemplate) PrepareMultilineString(s string) string {
	return strings.TrimRight(
		strings.TrimSpace(
			strings.ReplaceAll(s, "\n", " "),
		),
		".\r\n\t",
	)
}

func (tabularTemplate) Prettify(s string) string {
	var max = func(x, y int) int {
		if x > y {
			return x
		}
		return y
	}

	var b strings.Builder

	// search for tables
	for _, rawTable := range regexp.MustCompile(`(?m)^(\|[^\n]+\|\r?\n)((?:\|:?-+:?)+\|)(\n(?:\|[^\n]+\|\r?\n?)*)?$`).FindAllString(s, -1) {
		var lines = strings.FieldsFunc(rawTable, func(r rune) bool { return r == '\n' })

		if len(lines) < 3 { // header, separator, body
			continue
		}

		// parse table into the matrix
		var matrix = make([][]string, 0, len(lines))
		for _, line := range lines {
			items := strings.FieldsFunc(strings.Trim(line, "| "), func(r rune) bool { return r == '|' })

			for i := range items {
				items[i] = strings.TrimSpace(items[i]) // trim spaces in cells
			}

			matrix = append(matrix, items)
		}

		// determine centered columns
		var centered = make([]bool, 0, len(matrix[1]))
		for _, cell := range matrix[1] {
			centered = append(centered, strings.HasPrefix(cell, ":") && strings.HasSuffix(cell, ":"))
		}

		// calculate max lengths
		var lengths = make([]int, len(matrix[0]))
		for n, row := range matrix {
			for i, cell := range row {
				if n == 1 {
					continue // skip separator
				}

				if l := utf8.RuneCountInString(cell); l > lengths[i] {
					lengths[i] = l
				}
			}
		}

		// format cells
		for i, row := range matrix {
			for j, cell := range row {
				if i == 1 { // is separator
					if centered[j] {
						b.Reset()
						b.WriteRune(':')
						b.WriteString(strings.Repeat("-", max(0, lengths[j])))
						b.WriteRune(':')

						row[j] = b.String()
					} else {
						row[j] = strings.Repeat("-", max(0, lengths[j]+2))
					}

					continue
				}

				var (
					cellWidth         = utf8.RuneCountInString(cell)
					padLeft, padRight = 1, max(1, lengths[j]-cellWidth+1) // align to the left
				)

				if centered[j] { // is centered
					padLeft = max(1, (lengths[j]-cellWidth)/2)
					padRight = max(1, lengths[j]-cellWidth-(padLeft-1))
				}

				b.Reset()
				b.WriteString(strings.Repeat(" ", padLeft))

				if padLeft+cellWidth+padRight <= lengths[j]+1 {
					b.WriteRune(' ') // add an extra space if the cell is not full
				}

				b.WriteString(cell)
				b.WriteString(strings.Repeat(" ", padRight))

				row[j] = b.String()
			}
		}

		b.Reset()

		for _, row := range matrix { // build new table
			b.WriteRune('|')
			b.WriteString(strings.Join(row, "|"))
			b.WriteRune('|')
			b.WriteRune('\n')
		}

		s = strings.Replace(s, rawTable, b.String(), 1)
	}

	s = regexp.MustCompile(`\n{2,}`).ReplaceAllString(s, "\n\n") // normalize newlines
	s = strings.Trim(s, " \n")                                   // trim spaces and newlines

	return s + "\n" // add an extra newline
}
