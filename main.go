package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"runtime"
	"strings"
	"text/template"

	"github.com/alexflint/go-arg"
	"github.com/brentp/gargs/process"
	"github.com/brentp/xopen"
	"github.com/fatih/color"
	isatty "github.com/mattn/go-isatty"
)

// Version is the current version
const Version = "0.3.4-dev"

// ExitCode is the highest exit code seen in any command
var ExitCode = 0

// Params are the user-specified command-line arguments
type Params struct {
	Procs           int    `arg:"-p,help:number of processes to use."`
	Nlines          int    `arg:"-n,help:number of lines to consume for each command. -s and -n are mutually exclusive."`
	Retry           int    `arg:"-r,help:number of times to retry a command if it fails (default is 0)."`
	Command         string `arg:"positional,required,help:command to execute."`
	Sep             string `arg:"-s,help:regular expression split line with to fill multiple template spots default is not to split. -s and -n are mutually exclusive."`
	Verbose         bool   `arg:"-v,help:print commands to stderr before they are executed."`
	ContinueOnError bool   `arg:"-c,--continue-on-error,help:report errors but don't stop the entire execution (which is the default)."`
	DryRun          bool   `arg:"-d,--dry-run,help:print (but do not run) the commands"`
}

// hold the arguments for each call that fill the template.
type tmplArgs struct {
	Lines string
	Xs    []string
}

// isStdin checks if we are getting data from stdin.
func isStdin() bool {
	// http://stackoverflow.com/a/26567513
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func main() {
	args := Params{Procs: 1, Nlines: 1}
	p := arg.MustParse(&args)
	if args.Sep != "" && args.Nlines > 1 {
		p.Fail("must specify either sep (-s) or n-lines (-n), not both")
	}
	// if neither is specified then we default to whitespace
	if args.Nlines == 1 && args.Sep == "" {
		args.Sep = "\\s+"
	}
	if !isStdin() {
		fmt.Fprintln(os.Stderr, color.RedString("ERROR: expecting input on STDIN"))
		os.Exit(255)
	}
	runtime.GOMAXPROCS(args.Procs)
	run(args)
	os.Exit(ExitCode)
}

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func handleCommand(args *Params, cmd string, ch chan string) {
	if args.DryRun {
		fmt.Fprintf(os.Stdout, "%s\n", cmd)
		return
	}
	ch <- cmd
}

func genCommands(args *Params, tmpl *template.Template) <-chan string {
	ch := make(chan string)
	var resep *regexp.Regexp
	if args.Sep != "" {
		resep = regexp.MustCompile(args.Sep)
	}
	rdr, err := xopen.Ropen("-")
	check(err)

	go func() {
		re := regexp.MustCompile(`\r?\n`)
		lines := make([]string, 0, args.Nlines)
		var buf bytes.Buffer
		for {
			buf.Reset()
			line, err := rdr.ReadString('\n')
			if err == nil || (err == io.EOF && len(line) > 0) {
				line = re.ReplaceAllString(line, "")
				if resep != nil {
					toks := resep.Split(line, -1)
					check(tmpl.Execute(&buf, &tmplArgs{Xs: toks, Lines: line}))
					handleCommand(args, buf.String(), ch)
				} else {
					lines = append(lines, line)
				}
			} else {
				if err == io.EOF {
					break
				}
				log.Fatal(err)
			}
			if len(lines) == args.Nlines {
				check(tmpl.Execute(&buf, &tmplArgs{Lines: strings.Join(lines, " "), Xs: lines}))
				lines = lines[:0]
				handleCommand(args, buf.String(), ch)
			}
		}
		if len(lines) > 0 {
			check(tmpl.Execute(&buf, &tmplArgs{Lines: strings.Join(lines, " "), Xs: lines}))
			handleCommand(args, buf.String(), ch)
		}
		close(ch)
	}()
	return ch
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	color.NoColor = !isatty.IsTerminal(os.Stderr.Fd())
}

func run(args Params) {

	tmpl := makeCommandTmpl(args.Command)
	cmds := genCommands(&args, tmpl)

	stdout := bufio.NewWriter(os.Stdout)
	defer stdout.Flush()

	cancel := make(chan bool)
	defer close(cancel)

	for p := range process.Runner(cmds, args.Retry, cancel) {
		if ex := p.ExitCode(); ex != 0 {
			c := color.New(color.BgRed).Add(color.Bold)
			fmt.Fprintf(os.Stderr, "%s\n", c.SprintFunc()(fmt.Sprintf("ERROR with command: %s", p)))
			ExitCode = max(ExitCode, ex)
			if !args.ContinueOnError {
				break
			}
		}
		if args.Verbose {
			fmt.Fprintf(os.Stderr, "%s\n", p)
		}
		io.Copy(stdout, p)
	}

}

func makeCommandTmpl(cmd string) *template.Template {
	v := strings.Replace(cmd, "{}", "{{.Lines}}", -1)
	re := regexp.MustCompile(`({\d+})`)
	v = re.ReplaceAllStringFunc(v, func(match string) string {
		return "{{index .Xs " + match[1:len(match)-1] + "}}"
	})
	tmpl, err := template.New(v).Parse(v)
	check(err)
	return tmpl
}
