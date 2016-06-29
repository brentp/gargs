<!--
rm -rf binaries
mkdir -p binaries/
VERSION=0.1.0
for os in darwin linux windows; do
	GOOS=$os GOARCH=$arch go build -o binaries/gargs_${os} main.go
done
-->
gargs
=====

Work In Progress:

gargs is like xargs but it addresses the following limitations in xargs:

+ it keeps the output serialized even when using multiple threads
+ easy to specify multiple arguments

As an example that currently works, this will keep the output in order and send 3 arguments to each process.
It is using 4 proceses to parallelize.

```
$ seq 12 -1 1 | gargs -p 4 -n 3 "sleep {}; echo {} {}"
11 10
8 7
5 4
2 1
```

Note that for each line, we slept 12, 9, 6, 3 seconds respectively but the output order was maintained.


For now, the -n 3 is redundant with seeing the "{}"'s. In the future, it may be possible to use numbered arguments:

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
cat t.txt | gargs --sep "\s+" -p 2 "echo '{}:{}-{}'"
```

In this case, we're using **2** processes to run this in parallel which will make more of a difference
if we do something time-consuming rather than `echo`. The output will be kept in the order dictated by
`t.txt` even if the processes finish in a different order. This is sometimes at the expense of parallelization
efficiency.


Usage
=====

```
usage: gargs [--procs PROCS] [--nlines NLINES] [--sep SEP] [--shell SHELL] [--verbose] COMMAND

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
  --help, -h             display this help and exit
```

TODO
====

+ --unordered flag to specify that we don't care about the output order. Will improve parallelization for some cases.
+ {0}, {1}, {2} place-holders?
+ combinations of `-n` and `--sep`.
