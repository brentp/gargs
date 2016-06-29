<!--
rm -rf binaries
mkdir -p binaries/
VERSION=0.2.0
for os in darwin linux windows; do
	GOOS=$os GOARCH=$arch go build -o binaries/gargs_${os} main.go
done
-->
gargs
=====

Work In Progress:

gargs is like xargs but it addresses the following limitations in xargs:

+ it keeps the output serialized even when using multiple threads
+ easy to specify multiple arguments with number blocks ({0}, {1}, ...) and {} indicates the entire line.

This will keep the output in order (via -o) and send 3 arguments to each process
by pulling in lines of 3.
It is using 4 proceses to parallelize.

```
$ seq 12 -1 1 | gargs -o -p 4 -n 3 "sleep {0}; echo {1} {2}"
11 10
8 7
5 4
2 1
```

Note that for each line, we slept 12, 9, 6, 3 seconds respectively but the output order was maintained. We can make
more even use of cores by not enforcing the output order (remove -o)

```
$ seq 12 -1 1 | gargs -p 4 -n 3 "sleep {0}; echo {1} {2}"
2 1
5 4
8 7
11 10
```


The -n 3 indicates that we'll use 3 lines to fill the args. redundant with seeing the "{}"'s. In the future, it may be possible to use numbered arguments:

Install
=======

Download the appropriate binary for your system from [releases](https://github.com/brentp/gargs/releases) into your $PATH.


Example
=======
Let's say we have a file `t.txt` like:
```
chr1	22 33
chr2 22 33
chr3 22	33
chr4	22	33
```
That has a mixture of tabs and spaces. We can convert to chrom:start-end format with:

```
$ cat t.txt | gargs --sep "\s+" -p 2 "echo '{0}:{1}-{2}' full-line: \'{}\'"
chr2:22-33 full-line: 'chr2 22 33'
chr1:22-33 full-line: 'chr1 22 33'
chr3:22-33 full-line: 'chr3 22 33'
chr4:22-33 full-line: 'chr4 22 33'
```

In this case, we're using **2** processes to run this in parallel which will make more of a difference
if we do something time-consuming rather than `echo`. The output will be kept in the order dictated by
`t.txt` even if the processes finish in a different order. This is sometimes at the expense of parallelization
efficiency.


Usage
=====

```
usage: gargs [--procs PROCS] [--nlines NLINES] [--sep SEP] [--shell SHELL] [--verbose] [--continueonerror] [--ordered] COMMAND

positional arguments:
  command                command to execute

options:
  --procs PROCS, -p PROCS
                         number of processes to use [default: 1]
  --nlines NLINES, -n NLINES
                         number of lines to consume for each command. -s and -n are mutually exclusive. [default: 1]
  --sep SEP, -s SEP      regular expression split line with to fill multiple template spots default is not to split. -s and -n are mutually exclusive.
  --shell SHELL          shell to use [default: bash]
  --verbose, -v          print commands to stderr before they are executed.
  --continueonerror, -c
                         report errors but don't stop the entire execution (which is the default).
  --ordered, -o          keep output in order of input; default is to output in order of return which greatly improves parallelization.
  --help, -h             display this help and exit
```

TODO
====

+ combinations of `-n` and `--sep`.
