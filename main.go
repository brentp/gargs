package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
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

// Version is the current version
const Version = "0.3.2"

// ExitCode is the highest exit code seen in any command
var ExitCode = 0

// Params are the user-specified command-line arguments
type Params struct {
	Procs           int    `arg:"-p,help:number of processes to use."`
	Nlines          int    `arg:"-n,help:number of lines to consume for each command. -s and -n are mutually exclusive."`
	Command         string `arg:"positional,required,help:command to execute."`
	Sep             string `arg:"-s,help:regular expression split line with to fill multiple template spots default is not to split. -s and -n are mutually exclusive."`
	Verbose         bool   `arg:"-v,help:print commands to stderr before they are executed."`
	ContinueOnError bool   `arg:"-c,--continue-on-error,help:report errors but don't stop the entire execution (which is the default)."`
	Ordered         bool   `arg:"-o,help:keep output in order of input at cost of reduced parallelization; default is to output in order of return."`
	DryRun          bool   `arg:"-d,--dry-run,help:print (but do not run) the commands"`
}

// hold the arguments for each call that fill the template.
type tmplArgs struct {
	Lines []string
	Xs    []string
}

func main() {
	args := Params{Procs: 1, Nlines: 1}
	p := arg.MustParse(&args)
	if args.Sep != "" && args.Nlines > 1 {
		p.Fail("must specify either sep (-s) or n-lines (-n), not both")
	}
	if !xopen.IsStdin() {
		fmt.Fprintln(os.Stderr, "ERROR: expecting input on STDIN")
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

func genTmplArgs(n int, sep string) chan *tmplArgs {
	ch := make(chan *tmplArgs)
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
					ch <- &tmplArgs{Xs: toks, Lines: []string{line}}
				} else {
					lines[k] = line
					k++
				}
			} else {
				if err == io.EOF {
					break
				}
				log.Fatal(err)
			}
			if k == n {
				k = 0
				ch <- &tmplArgs{Lines: lines, Xs: lines}
				lines = make([]string, n)
			}
		}
		if k > 0 {
			ch <- &tmplArgs{Lines: lines[:k], Xs: lines}
		}
		close(ch)
	}()
	return ch
}

type lockWriter struct {
	mu *sync.Mutex
	*bufio.Writer

	emu *sync.Mutex
	err io.Writer
}

func run(args Params) {

	stdout := &lockWriter{
		&sync.Mutex{},
		bufio.NewWriter(os.Stdout),
		&sync.Mutex{},
		os.Stderr,
	}
	defer stdout.Flush()

	chXargs := genTmplArgs(args.Nlines, args.Sep)
	cmd := makeCommand(args.Command)
	var wg sync.WaitGroup
	wg.Add(args.Procs)

	for i := 0; i < args.Procs; i++ {
		go func() {
			defer wg.Done()
			for x := range chXargs {
				process(stdout, cmd, &args, x)
			}
		}()
	}

	wg.Wait()
}

func makeCommand(cmd string) string {
	v := strings.Replace(cmd, "{}", "{{index .Lines 0}}", -1)
	re := regexp.MustCompile(`({\d+})`)
	v = re.ReplaceAllStringFunc(v, func(match string) string {
		return "{{index .Xs " + match[1:len(match)-1] + "}}"
	})
	return v
}

func getShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	return shell
}

func process(stdout *lockWriter, cmdStr string, args *Params, tArgs *tmplArgs) {

	tmpl, err := template.New(cmdStr).Parse(cmdStr)
	check(err)

	handleError := func(err error) {
		if err == nil {
			return
		}
		var argString string
		if tArgs.Xs != nil && len(tArgs.Xs) > 0 {
			argString = strings.Join(tArgs.Xs, ",")
		} else {
			argString = strings.Join(tArgs.Lines, "|")
		}
		stdout.emu.Lock()
		fmt.Fprintf(stdout.err, "[===\nERROR in command: %s using args: %s\n%s\n===]\n", cmdStr, argString, err)
		stdout.emu.Unlock()
		if ex, ok := err.(*exec.ExitError); ok {
			if st, ok := ex.Sys().(syscall.WaitStatus); ok {
				if !args.ContinueOnError {
					os.Exit(st.ExitStatus())
				} else if st.ExitStatus() > ExitCode {
					ExitCode = st.ExitStatus()
				}
			}
		} else {
			if !args.ContinueOnError {
				os.Exit(1)
			} else {
				ExitCode = 1
			}
		}
	}

	var buf bytes.Buffer
	check(tmpl.Execute(&buf, tArgs))

	cmdStr = buf.String()

	if args.Verbose {
		stdout.emu.Lock()
		fmt.Fprintf(stdout.err, "command: %s\n", cmdStr)
		stdout.emu.Unlock()
	}
	if args.DryRun {
		stdout.mu.Lock()
		fmt.Fprintf(stdout, "%s\n", cmdStr)
		stdout.mu.Unlock()
		return
	}

	cmd := exec.Command(getShell(), "-c", cmdStr)
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	handleError(err)
	if err != nil {
		return
	}

	err = cmd.Start()
	handleError(err)
	if err != nil {
		return
	}

	// we pass any possible errors back on this channel
	// and exit the main function when the following goroutine closes it.
	errors := make(chan error)

	// try to read 1MB. If we get it all then we get ErrBufferFull
	// will this always be limited by size of bufio reader buffer?
	go func() {
		defer close(errors)
		defer func() { errors <- cmd.Wait() }()

		bPipe := bufio.NewReaderSize(pipe, 1048576)
		res, pErr := bPipe.Peek(1048576)
		//res, pErr := bPipe.Peek(2)
		if pErr == bufio.ErrBufferFull || pErr == io.EOF {
			stdout.mu.Lock()
			_, err = stdout.Write(res)
			stdout.mu.Unlock()
			if err != nil {
				errors <- err
			}
		} else if pErr != nil {
			errors <- pErr
		} else { // otherwise, we use temporary files.
			// TODO: tmpfiles sometimes get left if process is interrupted.
			// see how it's done in gsort in init() by adding common suffix.
			tmp, xerr := ioutil.TempFile("", "gargsTmp.")
			check(xerr)
			defer os.Remove(tmp.Name())
			bTmp := bufio.NewWriter(tmp)
			// copy the output of the command into the tmp file
			_, err = io.CopyBuffer(bTmp, bPipe, res)
			if err != nil {
				errors <- err
				return
			}
			errors <- pipe.Close()
			bTmp.Flush()

			// copy the tmp file to stdout.
			_, err := tmp.Seek(0, 0)
			errors <- err
			cTmp := bufio.NewReaderSize(tmp, 1048576)
			stdout.mu.Lock()
			_, err = io.CopyBuffer(stdout, cTmp, res)
			stdout.mu.Unlock()
			errors <- err
		}
	}()

	for e := range errors {
		handleError(e)
	}

}
