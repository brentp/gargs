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
	Procs           int    `arg:"-p,help:number of processes to use"`
	Nlines          int    `arg:"-n,help:number of lines to consume for each command. -s and -n are mutually exclusive."`
	Command         string `arg:"positional,required,help:command to execute"`
	Sep             string `arg:"-s,help:regular expression split line with to fill multiple template spots default is not to split. -s and -n are mutually exclusive."`
	Verbose         bool   `arg:"-v,help:print commands to stderr before they are executed."`
	ContinueOnError bool   `arg:"-c,--continue-on-error,help:report errors but don't stop the entire execution (which is the default)."`
	DryRun          bool   `arg:"-d,--dry-run,help:print (but do not run) the commands"`
}

// hold the arguments for each call that fill the template.
type tmplArgs struct {
	Lines []string
	Xs    []string
}

func main() {
	args := Params{Procs: 1, Nlines: 1}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	p := arg.MustParse(&args)
	if args.Sep != "" && args.Nlines > 1 {
		p.Fail("must specify either sep (-s) or n-lines (-n), not both")
	}
	if !xopen.IsStdin() {
		fmt.Fprintln(os.Stderr, "ERROR: expecting input on STDIN")
		os.Exit(255)
	}
	runtime.GOMAXPROCS(args.Procs)
	runUnOrdered(args, shell)
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

func runUnOrdered(args Params, shell string) {
	chXargs := genTmplArgs(args.Nlines, args.Sep)
	cmd := makeCommand(args.Command)
	mu := &sync.Mutex{}
	var wg sync.WaitGroup
	wg.Add(args.Procs)

	go func() {
		for i := 0; i < args.Procs; i++ {
			go func() {
				defer wg.Done()
				for x := range chXargs {
					process(mu, cmd, &args, x, shell)
				}
			}()
		}
	}()
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

func process(mu *sync.Mutex, cmdStr string, args *Params, tArgs *tmplArgs, shell string) {

	tmpl, err := template.New(cmdStr).Parse(cmdStr)
	check(err)

	var buf bytes.Buffer
	check(tmpl.Execute(&buf, tArgs))

	cmdStr = buf.String()

	if args.Verbose {
		fmt.Fprintf(os.Stderr, "command: %s\n", cmdStr)
	}
	if args.DryRun {
		mu.Lock()
		fmt.Fprintf(os.Stdout, "%s\n", cmdStr)
		mu.Unlock()
		return
	}

	cmd := exec.Command(shell, "-c", cmdStr)
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	var bPipe *bufio.Reader
	if err == nil {
		bPipe = bufio.NewReader(pipe)
		err = cmd.Start()
	}

	if err == nil {
		res, pErr := bPipe.Peek(8388608)
		if pErr == bufio.ErrBufferFull || pErr == io.EOF {
			mu.Lock()
			defer mu.Unlock()
			// TODO: buffer Stdout
			_, err = os.Stdout.Write(res)
		} else {
			tmp, xerr := ioutil.TempFile("", "gargs")
			check(xerr)
			defer os.Remove(tmp.Name())
			bTmp := bufio.NewWriter(tmp)
			_, err = io.Copy(bTmp, bPipe)
			defer tmp.Close()
			if err == nil {
				if err == nil {
					tmp.Seek(0, 0)
					cTmp := bufio.NewReader(tmp)
					mu.Lock()
					defer mu.Unlock()
					_, err = io.Copy(os.Stdout, cTmp)
				}

			}
		}
	}
	if err == nil {
		err = cmd.Wait()
	} else {
		cmd.Process.Kill()
	}

	if err != nil {
		var argString string
		if tArgs.Xs != nil && len(tArgs.Xs) > 0 {
			argString = strings.Join(tArgs.Xs, ",")
		} else {
			argString = strings.Join(tArgs.Lines, "|")
		}
		fmt.Fprintf(os.Stderr, "[===\nERROR in command: %s using args: %s\n%s\n===]\n", cmdStr, argString, err)
		ex := err.(*exec.ExitError)
		if st, ok := ex.Sys().(syscall.WaitStatus); ok {
			if !args.ContinueOnError {
				os.Exit(st.ExitStatus())
			} else if st.ExitStatus() > ExitCode {
				ExitCode = st.ExitStatus()
			}
		}
	}
}
