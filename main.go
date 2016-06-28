package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/brentp/xopen"
)

type Args struct {
	Procs   int    `arg:"-p,help:number of processes to use"`
	Nlines  int    `arg:"-n,help:number of lines to consume for each command"`
	Command string `arg:"positional,required,help:command to execute"`
	Sep     string `arg:"-s,help:split line(s) with this to fill multiple template spots default is not to split NOT IMPLEMENTED."`
	Shell   string `arg:"help:shell to use"`
	Verbose bool   `arg:"-v,help:print commands to stderr before they are executed."`
}

func main() {

	args := Args{}
	args.Procs = 1
	args.Nlines = 1
	args.Sep = ""
	args.Shell = "bash"
	args.Verbose = false
	arg.MustParse(&args)
	if !xopen.IsStdin() {
		fmt.Fprintln(os.Stderr, "ERROR: expecting input on STDIN")
		os.Exit(255)
	}
	runtime.GOMAXPROCS(args.Procs)
	run(args)
}

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}

func genLines(n int, sep string) chan []interface{} {
	ch := make(chan []interface{})
	go func() {
		rdr, err := xopen.Ropen("-")
		check(err)
		k := 0
		lines := make([]interface{}, n)
		re := regexp.MustCompile(`\r?\n`)
		for {
			line, err := rdr.ReadString('\n')
			if err == nil || (err == io.EOF && len(line) > 0) {
				lines[k] = re.ReplaceAllString(line, "")
				k += 1
			} else {
				if err == io.EOF {
					break
				}
				log.Fatal(err)
			}
			if k == n {
				k = 0
				ch <- lines
				lines = make([]interface{}, n)
			}
		}
		if k > 0 {
			ch <- lines[:k]
		}
		close(ch)
	}()
	return ch
}

func run(args Args) {
	ch := make(chan chan []byte, args.Procs)

	chlines := genLines(args.Nlines, args.Sep)
	cmd := makeCommand(args.Command)

	go func() {
		for lines := range chlines {
			ich := make(chan []byte, 1)
			ch <- ich
			go process(ich, cmd, args, lines)
		}
		close(ch)
	}()

	for o := range ch {
		os.Stdout.Write(<-o)
	}
}

func makeCommand(cmd string) string {
	return strings.Replace(strings.Replace(cmd, "%", "%%", -1), "{}", "%s", -1)
}

func process(ch chan []byte, cmdStr string, args Args, lines []interface{}) {
	cmdStr = fmt.Sprintf(cmdStr, lines...)
	if args.Verbose {
		fmt.Fprintf(os.Stderr, "command: %s\n", cmdStr)
	}

	cmd := exec.Command(args.Shell, "-c", cmdStr)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	check(err)
	ch <- out
	close(ch)
}
