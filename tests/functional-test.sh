#!/bin/bash

test -e ssshtest || wget -q https://raw.githubusercontent.com/ryanlayer/ssshtest/master/ssshtest

. ssshtest
set -e

echo $ORDERED

go build -o gargs_race -race

set +e

fn_check_env() {
    seq 5 10 | ./gargs_race $ORDERED -p 2 'set -u; echo $PROCESS_I'
}
run check_env fn_check_env
assert_exit_code 0
assert_equal 1 $(grep -wc 0 $STDOUT_FILE)
assert_equal 1 $(grep -wc 1 $STDOUT_FILE)
assert_equal 1 $(grep -wc 2 $STDOUT_FILE)
assert_equal 1 $(grep -wc 3 $STDOUT_FILE)
assert_equal 1 $(grep -wc 4 $STDOUT_FILE)
assert_equal 1 $(grep -wc 5 $STDOUT_FILE)

fn_check_ordered() {
     seq 1 500 | ./gargs_race -o -p 20  'echo {}' | md5sum
}

run check_ordered fn_check_ordered
assert_exit_code 0
assert_in_stdout $(seq 1 500 | md5sum | cut -f 1 -d" ")


fn_check_basic() {
	seq 12 -1 1 | ./gargs_race $ORDERED -p 5 -n 3 -v 'sleep {0}; echo {1} {2}'
}
run check_basic fn_check_basic
assert_exit_code 0
assert_in_stderr 'Command('
assert_equal 4 $(wc -l $STDOUT_FILE)
assert_equal 4 $(grep -c sleep $STDERR_FILE)

fn_check_log() {
	seq 3 -1 0 | ./gargs_race -l __o.log "python -c 'print 1/{}'"
}

fn_check_logok() {
	seq 1 3 | ./gargs_race -l __o.log "python -c 'print 1/{}'"
}
fn_check_loghead() {
	seq 1 100 | ./gargs_race -l __o.log "echo {}" | head
}

run check_log fn_check_log
assert_exit_code 1
assert_equal 5 $(cat __o.log | wc -l)
assert_equal 4 $(grep -c ^# __o.log)
assert_equal 1 $(grep -c "^python -c 'print 1/0'" __o.log)
assert_equal 1 $(grep -c "^# FAILED 1 commands" __o.log)
rm -f __o.log

run check_log_ok fn_check_logok
assert_exit_code 0
assert_equal 1 $(grep -c "^# SUCCESS" __o.log)
rm -f __o.log

# make sure we dont print success even if everything didn't finish.
run check_log_head fn_check_loghead
assert_equal 0 $(grep -c "^# SUCCESS" __o.log)
rm -f __o.log

fn_check_sep() {
	set -o pipefail
	cat tests/t.txt | ./gargs_race $ORDERED -p 2 "echo -e '{0}:{1}-{2}' full-line: \'{}\'"
}
run check_sep fn_check_sep
assert_exit_code 0
assert_in_stdout "chr2:22-33 full-line: 'chr2 22 33'"
assert_in_stdout "chr1:22-33 full-line: 'chr1 22 33'"
assert_in_stdout "chr3:22-33 full-line: 'chr3 22 33'"
assert_in_stdout "chr4:22-33 full-line: 'chr4 22 33'"


fn_check_exit_err(){
	seq 0 5  | ./gargs_race $ORDERED -p 5 "python -c 'print 1.0/{}'"
}
run check_exit_err fn_check_exit_err
assert_exit_code 1
assert_in_stdout "0.2"
assert_in_stderr "ZeroDivisionError"


fn_custom_shell(){
	seq 0 5 | SHELL=python ./gargs_race $ORDERED "print '%.2f' % {}"
}
run check_custom_shell fn_custom_shell
assert_exit_code 0
assert_in_stdout "1.00"
assert_equal "6" $(wc -l $STDOUT_FILE)

fn_test_filehandles(){
	seq 1 2000 | ./gargs_race $ORDERED -p 5 "echo {}"
}
run check_filehandles fn_test_filehandles
assert_exit_code 0



# different code-path that uses tmpfiles if we have > 4MB of data for each
fn_test_big() {
	seq 10 | SHELL=python go run main.go  "for i in range(100): print ''.join('{}' for i in xrange(90000))"
}
run check_big fn_test_big
assert_exit_code 0
assert_equal 1000 $(cat $STDOUT_FILE | wc -l)


if [[ ! -z "$ORDERED" ]]; then
	# test ordering

fn_test_order() {
	seq 10 -1 1 | ./gargs_race -o -p 10 "sleep {}; echo {}"
}

fi


fn_check_retries() {
	seq 0 10 | ./gargs_race --retry 3 "python -c '1/{}'"
}
run check_retries fn_check_retries
assert_exit_code 1
assert_equal $(grep -c ZeroDivisionError $STDERR_FILE) "4"

fn_check_nlines2() {
	seq 1 10 | ./gargs_race -n 5  "echo {} blah"
}

# we did 5 per so we should have 2 lines.
run check_nlines2 fn_check_nlines2
assert_exit_code 0
assert_equal $(cat $STDOUT_FILE | wc -l) 2

fn_check_nlines4() {
	seq 1 10 | ./gargs_race --dry-run -n 3  "{}"
}

run check_nlines4 fn_check_nlines4
assert_exit_code 0
assert_equal $(cat $STDOUT_FILE | wc -l) 4
assert_in_stdout "7 8 9"
assert_equal $(grep -c "^10$" $STDOUT_FILE) 1

# time out
fn_check_timeout() {
	seq 1 | ./gargs_race -t 1 "sleep 5; echo asdf"
}
run fn_check_timeout
assert_no_stdout
