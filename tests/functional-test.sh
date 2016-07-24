#!/bin/bash

test -e ssshtest || wget -q https://raw.githubusercontent.com/ryanlayer/ssshtest/master/ssshtest

. ssshtest
set -e

go build -o gargs_race -race -a

set +e

fn_check_basic() {
	seq 12 -1 1 | ./gargs_race -p 5 -n 3 -d  -v 'sleep {0}; echo {1} {2}'
}
run check_basic fn_check_basic
assert_exit_code 0
assert_in_stderr 'command:'
assert_equal 4 $(wc -l $STDOUT_FILE)
assert_equal 4 $(grep -c sleep $STDOUT_FILE)

fn_check_sep() {
	cat tests/t.txt | ./gargs_race --sep "\s+" -p 2 "echo -e '{0}:{1}-{2}' full-line: \'{}\'"
}
run check_sep fn_check_sep
assert_exit_code 0
assert_in_stdout "chr2:22-33 full-line: 'chr2 22 33'"
assert_in_stdout "chr1:22-33 full-line: 'chr1 22 33'"
assert_in_stdout "chr3:22-33 full-line: 'chr3 22 33'"
assert_in_stdout "chr4:22-33 full-line: 'chr4 22 33'"


fn_check_exit_err(){
	seq 0 5  | ./gargs_race -p 5 "python -c 'print 1.0/{}'"
}
run check_exit_err fn_check_exit_err
assert_exit_code 1
assert_in_stderr "ZeroDivisionError"


fn_custom_shell(){
	seq 0 5 | SHELL=python ./gargs_race -c "print '%.2f' % {}"
}
run check_custom_shell fn_custom_shell
assert_exit_code 0
assert_in_stdout "1.00"
assert_equal "6" $(wc -l $STDOUT_FILE)

go build -o gargs_fast -a
fn_test_filehandles(){
	seq 1 10000 | ./gargs_fast -p 5 "echo {}"
}
run check_filehandles fn_test_filehandles
assert_exit_code 0