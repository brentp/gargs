package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"text/template"

	"github.com/alexflint/go-arg"
	"github.com/brentp/xopen"
)

const VERSION = "0.3.1-dev"

var EXIT_CODE = 0

type Args struct {
	Procs           int    `arg:"-p,help:number of processes to use"`
	Nlines          int    `arg:"-n,help:number of lines to consume for each command. -s and -n are mutually exclusive."`
	Command         string `arg:"positional,required,help:command to execute"`
	Sep             string `arg:"-s,help:regular expression split line with to fill multiple template spots default is not to split. -s and -n are mutually exclusive."`
	Shell           string `arg:"help:shell to use"`
	Verbose         bool   `arg:"-v,help:print commands to stderr before they are executed."`
	ContinueOnError bool   `arg:"-c,--continue-on-error,help:report errors but don't stop the entire execution (which is the default)."`
	Ordered         bool   `arg:"-o,help:keep output in order of input; default is to output in order of return which greatly improves parallelization."`
	DryRun          bool   `arg:"-d,--dry-run,help:print (but do not run) the commands"`
}

// hold the arguments for each call.
type xargs struct {
	Lines []string
	Xs    []string
}

func main() {

	args := Args{}
	args.Procs = 1
	args.Nlines = 1
	args.Sep = ""
	args.Shell = "bash"
	args.Verbose = false
	args.ContinueOnError = false
	args.Ordered = false
	args.DryRun = false
	p := arg.MustParse(&args)
	if args.Sep != "" && args.Nlines > 1 {
		p.Fail("must specify either sep (-s) or n-lines (-n), not both")
	}
	if !xopen.IsStdin() {
		fmt.Fprintln(os.Stderr, "ERROR: expecting input on STDIN")
		os.Exit(255)
	}
	runtime.GOMAXPROCS(args.Procs)
	if args.Ordered {
		runOrdered(args)
	} else {
		runUnOrdered(args)
	}
	os.Exit(EXIT_CODE)
}

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func genXargs(n int, sep string) chan *xargs {
	ch := make(chan *xargs)
	var resep *regexp.Regexp
	if sep != "" {
		resep = regexp.MustCompile(sep)
	}

	go func() {
		rdr, err := xopen.Ropen("-")
		check(err)
		k := 0
		re := regexp.MustCompile(`\r?\n`)
		lines := make([]string, n)

		for {
			line, err := rdr.ReadString('\n')
			if err == nil || (err == io.EOF && len(line) > 0) {
				line = re.ReplaceAllString(line, "")
				if resep != nil {
					toks := resep.Split(line, -1)
					ch <- &xargs{Xs: toks, Lines: []string{line}}
				} else {
					lines[k] = line
					k += 1
				}
			} else {
				if err == io.EOF {
					break
				}
				log.Fatal(err)
			}
			if k == n {
				k = 0
				ch <- &xargs{Lines: lines, Xs: lines}
				lines = make([]string, n)
			}
		}
		if k > 0 {
			ch <- &xargs{Lines: lines[:k], Xs: lines}
		}
		close(ch)
	}()
	return ch
}

func runUnOrdered(args Args) {
	c := make(chan []byte)
	chXargs := genXargs(args.Nlines, args.Sep)
	cmd := makeCommand(args.Command)

	go func() {
		var wg sync.WaitGroup
		wg.Add(args.Procs)
		for i := 0; i < args.Procs; i++ {
			go func() {
				defer wg.Done()
				for {
					x, ok := <-chXargs
					if !ok {
						return
					}
					process(c, cmd, args, x)
				}
			}()

		}
		wg.Wait()
		close(c)
	}()

	for o := range c {
		os.Stdout.Write(o)
	}
}

func runOrdered(args Args) {
	ch := make(chan chan []byte, args.Procs)

	chXargs := genXargs(args.Nlines, args.Sep)
	cmd := makeCommand(args.Command)

	go func() {
		for xa := range chXargs {
			ich := make(chan []byte, 1)
			ch <- ich
			go func(ich chan []byte, x *xargs) {
				process(ich, cmd, args, x)
				close(ich)
			}(ich, xa)
		}
		close(ch)
	}()

	for o := range ch {
		os.Stdout.Write(<-o)
	}
}

func makeCommand(cmd string) string {
	//return strings.Replace(strings.Replace(cmd, "%", "%%", -1), "{}", "%s", -1)
	v := strings.Replace(cmd, "{}", "{{index .Lines 0}}", -1)
	re := regexp.MustCompile(`({\d+})`)
	v = re.ReplaceAllStringFunc(v, func(match string) string {
		return "{{index .Xs " + match[1:len(match)-1] + "}}"
	})
	return v
}

func process(ch chan []byte, cmdStr string, args Args, xarg *xargs) {

	tmpl, err := template.New(cmdStr).Parse(cmdStr)
	check(err)

	var buf bytes.Buffer
	check(tmpl.Execute(&buf, xarg))

	cmdStr = buf.String()

	if args.Verbose {
		fmt.Fprintf(os.Stderr, "command: %s\n", cmdStr)
	}
	if args.DryRun {
		fmt.Fprintf(os.Stdout, "%s\n", cmdStr)
		return
	}

	cmd := exec.Command(args.Shell, "-c", cmdStr)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		var argString string
		if xarg.Xs != nil && len(xarg.Xs) > 0 {
			argString = strings.Join(xarg.Xs, ",")
		} else {
			argString = strings.Join(xarg.Lines, "|")
		}
		log.Printf("ERROR in command: %s\twith args: %s", cmdStr, argString)
		log.Println(err)
		if ex, ok := err.(*exec.ExitError); ok {
			if st, ok := ex.Sys().(syscall.WaitStatus); ok {
				if !args.ContinueOnError {
					os.Exit(st.ExitStatus())
				} else if st.ExitStatus() > EXIT_CODE {
					EXIT_CODE = st.ExitStatus()
				}
			}
		} else {
			if !args.ContinueOnError {
				os.Exit(1)
			} else {
				EXIT_CODE = 1
			}
		}
	}
	ch <- out
}
