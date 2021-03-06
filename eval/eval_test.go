package eval

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"syscall"
	"testing"

	"github.com/elves/elvish/daemon/api"
	"github.com/elves/elvish/parse"
	"github.com/elves/elvish/util"
)

func TestBuiltinPid(t *testing.T) {
	pid := strconv.Itoa(syscall.Getpid())
	builtinPid := ToString(makeBuiltinNamespace(nil)["pid"].Get())
	if builtinPid != pid {
		t.Errorf(`ev.builtin["pid"] = %v, want %v`, builtinPid, pid)
	}
}

type want struct {
	out      []Value
	bytesOut []byte
	err      error
}

var (
	wantNothing = want{}
	// Special value for want.err to indicate that any error, as long as not
	// nil, is OK
	errAny = errors.New("")
)

var filesToCreate = sorted(
	"a1", "a2", "a3", "a10", "b1", "b2", "b3",
	"foo", "bar", "lorem", "ipsum",
)

func sorted(a ...string) []string {
	sort.Strings(a)
	return a
}

var evalTests = []struct {
	text string
	want
}{
	// Chunks
	// ------

	// Empty chunk
	{"", wantNothing},
	// Outputs of pipelines in a chunk are concatenated
	{"put x; put y; put z", want{out: strs("x", "y", "z")}},
	// A failed pipeline cause the whole chunk to fail
	{"put a; e:false; put b", want{out: strs("a"), err: errAny}},

	// Pipelines
	// ---------

	// Pure byte pipeline
	{`echo "Albert\nAllan\nAlbraham\nBerlin" | sed s/l/1/g | grep e`,
		want{bytesOut: []byte("A1bert\nBer1in\n")}},
	// Pure channel pipeline
	{`put 233 42 19 | each [x]{+ $x 10}`, want{out: strs("243", "52", "29")}},
	// Pipeline draining.
	{`range 100 | put x`, want{out: strs("x")}},
	// TODO: Add a useful hybrid pipeline sample

	// Assignments
	// -----------

	// List element assignment
	{"li=[foo bar]; li[0]=233; put $@li",
		want{out: strs("233", "bar")}},
	// Map element assignment
	{"di=[&k=v]; di[k]=lorem; di[k2]=ipsum; put $di[k] $di[k2]",
		want{out: strs("lorem", "ipsum")}},
	{"d=[&a=[&b=v]]; put $d[a][b]; d[a][b]=u; put $d[a][b]",
		want{out: strs("v", "u")}},
	// Multi-assignments.
	{"{a,b}=(put a b); put $a $b", want{out: strs("a", "b")}},
	{"@a=(put a b); put $@a", want{out: strs("a", "b")}},
	{"{a,@b}=(put a b c); put $@b", want{out: strs("b", "c")}},
	//{"di=[&]; di[a b]=(put a b); put $di[a] $di[b]", want{out: strs("a", "b")}},

	// Temporary assignment.
	{"a=alice b=bob; {a,@b}=(put amy ben) put $a $@b; put $a $b",
		want{out: strs("amy", "ben", "alice", "bob")}},
	// Temporary assignment of list element.
	{"l = [a]; l[0]=x put $l[0]; put $l[0]", want{out: strs("x", "a")}},
	// Temporary assignment of map element.
	{"m = [&k=v]; m[k]=v2 put $m[k]; put $m[k]", want{out: strs("v2", "v")}},
	// Temporary assignment before special form.
	{"li=[foo bar] for x $li { put $x }", want{out: strs("foo", "bar")}},

	// Spacey assignment.
	{"a @b = 2 3 foo; put $a $b[1]", want{out: strs("2", "foo")}},
	// Spacey assignment with temporary assignment
	{"x = 1; x=2 y = (+ 1 $x); put $x $y", want{out: strs("1", "3")}},

	// Control structures
	// ------------------

	// if
	{"if true { put then }", want{out: strs("then")}},
	{"if $false { put then } else { put else }", want{out: strs("else")}},
	{"if $false { put 1 } elif $false { put 2 } else { put 3 }",
		want{out: strs("3")}},
	{"if $false { put 2 } elif true { put 2 } else { put 3 }",
		want{out: strs("2")}},
	// try
	{"try { nop } except { put bad } else { put good }", want{out: strs("good")}},
	{"try { e:false } except - { put bad } else { put good }", want{out: strs("bad")}},
	// while
	{"x=0; while (< $x 4) { put $x; x=(+ $x 1) }",
		want{out: strs("0", "1", "2", "3")}},
	// for
	{"for x [tempora mores] { put 'O '$x }",
		want{out: strs("O tempora", "O mores")}},
	// break
	{"for x [a] { break } else { put $x }", wantNothing},
	// else
	{"for x [a] { put $x } else { put $x }", want{out: strs("a")}},
	// continue
	{"for x [a b] { put $x; continue; put $x; }", want{out: strs("a", "b")}},

	// Redirections
	// ------------

	{"f=(mktemp elvXXXXXX); echo 233 > $f; cat < $f; rm $f",
		want{bytesOut: []byte("233\n")}},

	// Redirections from special form.
	{`f = (mktemp elvXXXXXX);
	for x [lorem ipsum] { echo $x } > $f
	cat $f
	rm $f`,
		want{bytesOut: []byte("lorem\nipsum\n")}},

	// Redirections from File object.
	{`fname=(mktemp elvXXXXXX); echo haha > $fname;
			f=(fopen $fname); cat <$f; fclose $f; rm $fname`,
		want{bytesOut: []byte("haha\n")}},

	// Redirections from Pipe object.
	{`p=(pipe); echo haha > $p; pwclose $p; cat < $p; prclose $p`,
		want{bytesOut: []byte("haha\n")}},

	// Compounding
	// -----------
	{"put {fi,elvi}sh{1.0,1.1}",
		want{out: strs("fish1.0", "fish1.1", "elvish1.0", "elvish1.1")}},

	// List, Map and Indexing
	// ----------------------

	{"echo [a b c] [&key=value] | each put",
		want{out: strs("[a b c] [&key=value]")}},
	{"put [a b c][2]", want{out: strs("c")}},
	{"put [&key=value][key]", want{out: strs("value")}},

	// String Literals
	// ---------------
	{`put 'such \"''literal'`, want{out: strs(`such \"'literal`)}},
	{`put "much \n\033[31;1m$cool\033[m"`,
		want{out: strs("much \n\033[31;1m$cool\033[m")}},

	// Captures
	// ---------

	// Output capture
	{"put (put lorem ipsum)", want{out: strs("lorem", "ipsum")}},
	{"put (print \"lorem\nipsum\")", want{out: strs("lorem", "ipsum")}},

	// Exception capture
	{"bool ?(nop); bool ?(e:false)", want{out: bools(true, false)}},

	// Variable Use
	// ------------

	// Compounding
	{"x='SHELL'\nput 'WOW, SUCH '$x', MUCH COOL'\n",
		want{out: strs("WOW, SUCH SHELL, MUCH COOL")}},
	// Splicing
	{"x=[elvish rules]; put $@x", want{out: strs("elvish", "rules")}},

	// Wildcard
	// --------

	{"put *", want{out: strs(filesToCreate...)}},
	{"put a/b/nonexistent*", want{err: ErrWildcardNoMatch}},
	{"put a/b/nonexistent*[nomatch-ok]", wantNothing},

	// Tilde
	// -----
	{"h=$E:HOME; E:HOME=/foo; put ~ ~/src; E:HOME=$h",
		want{out: strs("/foo", "/foo/src")}},

	// Closure
	// -------

	{"[]{ }", wantNothing},
	{"[x]{put $x} foo", want{out: strs("foo")}},

	// Variable capture
	{"x=lorem; []{x=ipsum}; put $x", want{out: strs("ipsum")}},
	{"x=lorem; []{ put $x; x=ipsum }; put $x",
		want{out: strs("lorem", "ipsum")}},

	// Shadowing
	{"x=ipsum; []{ local:x=lorem; put $x }; put $x",
		want{out: strs("lorem", "ipsum")}},

	// Shadowing by argument
	{"x=ipsum; [x]{ put $x; x=BAD } lorem; put $x",
		want{out: strs("lorem", "ipsum")}},

	// Closure captures new local variables every time
	{`fn f []{ x=0; put []{x=(+ $x 1)} []{put $x} }
		      {inc1,put1}=(f); $put1; $inc1; $put1
			  {inc2,put2}=(f); $put2; $inc2; $put2`,
		want{out: strs("0", "1", "0", "1")}},

	// fn.
	{"fn f [x]{ put x=$x'.' }; f lorem; f ipsum",
		want{out: strs("x=lorem.", "x=ipsum.")}},
	// return.
	{"fn f []{ put a; return; put b }; f", want{out: strs("a")}},

	// Rest argument.
	{"[x @xs]{ put $x $xs } a b c",
		want{out: []Value{String("a"), NewList(String("b"), String("c"))}}},
	// Options.
	{"[a &k=v]{ put $a $k } foo &k=bar", want{out: strs("foo", "bar")}},
	// Option default value.
	{"[a &k=v]{ put $a $k } foo", want{out: strs("foo", "v")}},

	// Namespaces
	// ----------

	// Pseudo-namespaces local: and up:
	{"x=lorem; []{local:x=ipsum; put $up:x $local:x}",
		want{out: strs("lorem", "ipsum")}},
	{"x=lorem; []{up:x=ipsum; put $x}; put $x",
		want{out: strs("ipsum", "ipsum")}},
	// Pseudo-namespace E:
	{"E:FOO=lorem; put $E:FOO", want{out: strs("lorem")}},
	{"del E:FOO; put $E:FOO", want{out: strs("")}},
	// TODO: Test module namespace

	// Builtin functions
	// -----------------

	{"kind-of bare 'str' [] [&] []{ }",
		want{out: strs("string", "string", "list", "map", "fn")}},

	{`put foo bar`, want{out: strs("foo", "bar")}},
	{`explode [foo bar]`, want{out: strs("foo", "bar")}},

	{`print [foo bar]`, want{bytesOut: []byte("[foo bar]")}},
	{`echo [foo bar]`, want{bytesOut: []byte("[foo bar]\n")}},
	{`pprint [foo bar]`, want{bytesOut: []byte("[\n foo\n bar\n]\n")}},

	{`print "a\nb" | slurp`, want{out: strs("a\nb")}},
	{`print "a\nb" | from-lines`, want{out: strs("a", "b")}},
	{`print "a\nb\n" | from-lines`, want{out: strs("a", "b")}},
	{`echo '{"k": "v", "a": [1, 2]}' '"foo"' | from-json`,
		want{out: []Value{
			ConvertToMap(map[Value]Value{
				String("k"): String("v"),
				String("a"): NewList(strs("1", "2")...)}),
			String("foo"),
		}}},
	{`echo 'invalid' | from-json`, want{err: errAny}},

	{`put "l\norem" ipsum | to-lines`,
		want{bytesOut: []byte("l\norem\nipsum\n")}},
	{`put [&k=v &a=[1 2]] foo | to-json`,
		want{bytesOut: []byte(`{"a":["1","2"],"k":"v"}
"foo"
`)}},

	{`joins : [/usr /bin /tmp]`, want{out: strs("/usr:/bin:/tmp")}},
	{`splits : /usr:/bin:/tmp`, want{out: strs("/usr", "/bin", "/tmp")}},
	{`replaces : / ":usr:bin:tmp"`, want{out: strs("/usr/bin/tmp")}},
	{`replaces &max=2 : / :usr:bin:tmp`, want{out: strs("/usr/bin:tmp")}},
	{`has-prefix golang go`, want{out: bools(true)}},
	{`has-prefix golang x`, want{out: bools(false)}},
	{`has-suffix golang x`, want{out: bools(false)}},

	{`keys [&]`, wantNothing},
	{`keys [&a=foo]`, want{out: strs("a")}},
	// Windows does not have an external sort command. Disabled until we have a
	// builtin sort command.
	// {`keys [&a=foo &b=bar] | each echo | sort | each put`, want{out: strs("a", "b")}},

	{`==s haha haha`, want{out: bools(true)}},
	{`==s 10 10.0`, want{out: bools(false)}},
	{`<s a b`, want{out: bools(true)}},
	{`<s 2 10`, want{out: bools(false)}},

	{`run-parallel { put lorem } { echo ipsum }`,
		want{out: strs("lorem"), bytesOut: []byte("ipsum\n")}},

	{`fail haha`, want{err: errAny}},
	{`return`, want{err: Return}},

	{`f=(constantly foo); $f; $f`, want{out: strs("foo", "foo")}},
	{`(constantly foo) bad`, want{err: errAny}},
	{`put 1 233 | each put`, want{out: strs("1", "233")}},
	{`echo "1\n233" | each put`, want{out: strs("1", "233")}},
	{`each put [1 233]`, want{out: strs("1", "233")}},
	{`range 10 | each [x]{ if (== $x 4) { break }; put $x }`,
		want{out: strs("0", "1", "2", "3")}},
	{`range 10 | each [x]{ if (== $x 4) { fail haha }; put $x }`,
		want{out: strs("0", "1", "2", "3"), err: errAny}},
	{`repeat 4 foo`, want{out: strs("foo", "foo", "foo", "foo")}},
	// TODO: test peach

	{`range 3`, want{out: strs("0", "1", "2")}},
	{`range 1 3`, want{out: strs("1", "2")}},
	{`range 0 10 &step=3`, want{out: strs("0", "3", "6", "9")}},
	{`put foo bar | all`, want{out: strs("foo", "bar")}},
	{`echo foobar | all`, want{bytesOut: []byte("foobar\n")}},
	{`{ put foo bar; echo foobar } | all`,
		want{out: strs("foo", "bar"), bytesOut: []byte("foobar\n")}},
	{`range 100 | take 2`, want{out: strs("0", "1")}},
	{`range 100 | drop 98`, want{out: strs("98", "99")}},
	{`range 100 | count`, want{out: strs("100")}},
	{`count [(range 100)]`, want{out: strs("100")}},

	{`echo "  ax  by cz  \n11\t22 33" | eawk [@a]{ put $a[-1] }`,
		want{out: strs("cz", "33")}},

	{`path-base a/b/c.png`, want{out: strs("c.png")}},

	// TODO test more edge cases
	{"+ 233100 233", want{out: strs("233333")}},
	{"- 233333 233100", want{out: strs("233")}},
	{"- 233", want{out: strs("-233")}},
	{"* 353 661", want{out: strs("233333")}},
	{"/ 233333 353", want{out: strs("661")}},
	{"/ 1 0", want{out: strs("+Inf")}},
	{"^ 16 2", want{out: strs("256")}},
	{"% 23 7", want{out: strs("2")}},

	{`== 1 1.0`, want{out: bools(true)}},
	{`== 10 0xa`, want{out: bools(true)}},
	{`== a a`, want{err: errAny}},
	{`> 0x10 1`, want{out: bools(true)}},

	{`is 1 1`, want{out: bools(true)}},
	{`is [] []`, want{out: bools(true)}},
	{`is [1] [1]`, want{out: bools(false)}},
	{`eq 1 1`, want{out: bools(true)}},
	{`eq [] []`, want{out: bools(true)}},

	{`ord a`, want{out: strs("0x61")}},
	{`base 16 42 233`, want{out: strs("2a", "e9")}},
	{`wcswidth 你好`, want{out: strs("4")}},
	{`has-key [foo bar] 0`, want{out: bools(true)}},
	{`has-key [foo bar] 0:1`, want{out: bools(true)}},
	{`has-key [foo bar] 0:20`, want{out: bools(false)}},
	{`has-key [&lorem=ipsum &foo=bar] lorem`, want{out: bools(true)}},
	{`has-key [&lorem=ipsum &foo=bar] loremwsq`, want{out: bools(false)}},
	{`has-value [&lorem=ipsum &foo=bar] lorem`, want{out: bools(false)}},
	{`has-value [&lorem=ipsum &foo=bar] bar`, want{out: bools(true)}},
	{`has-value [foo bar] bar`, want{out: bools(true)}},
	{`has-value [foo bar] badehose`, want{out: bools(false)}},
	{`has-value "foo" o`, want{out: bools(true)}},
	{`has-value "foo" d`, want{out: bools(false)}},

	{`put (assoc [0] 0 zero)[0]`, want{out: strs("zero")}},
	{`put (assoc [&] k v)[k]`, want{out: strs("v")}},
	{`put (assoc [&k=v] k v2)[k]`, want{out: strs("v2")}},
	{`has-key (dissoc [&k=v] k) k`, want{out: bools(false)}},

	// Modules (see setup_datadir_test.go for setup)
	// "use" imports a module.
	{`use lorem; put $lorem:name`, want{out: strs("lorem")}},
	// imports are lexically scoped
	// TODO: Support testing for compilation error
	// {`{ use lorem }; put $lorem:name`, want{err: errAny}},

	// use of imported variable is captured in upvalue
	{`({ use lorem; put { { put $lorem:name } } })`, want{out: strs("lorem")}},
	// use of imported function is also captured in upvalue
	{`{ use lorem; { lorem:put-name } }`, want{out: strs("lorem")}},

	// multi-level module names
	{`use a:b:c:d; put $a:b:c:d:name`, want{out: strs("a/b/c/d")}},
	// shortening module names by using slashes for some path prefix
	{`use a:b/c:d; put $c:d:name`, want{out: strs("a/b/c/d")}},
	// importing the same module under different names
	{`use a/b/c/d; use a/b/c:d; eq $d:name $c:d:name`, want{out: bools(true)}},
	// module is cached after first use
	{`use has/init; use has:init`, want{out: strs("has/init")}},
	// overriding module
	{`use d; put $d:name; use a/b/c/d; put $d:name`,
		want{out: strs("d", "a/b/c/d")}},
	// relative uses
	{`use a/b/c/x; put $x:d $x:lorem`, want{out: strs("a/b/c/d", "lorem")}},
}

func strs(ss ...string) []Value {
	vs := make([]Value, len(ss))
	for i, s := range ss {
		vs[i] = String(s)
	}
	return vs
}

func bools(bs ...bool) []Value {
	vs := make([]Value, len(bs))
	for i, b := range bs {
		vs[i] = Bool(b)
	}
	return vs
}

func mustParseAndCompile(t *testing.T, ev *Evaler, name, text string) Op {
	n, err := parse.Parse(name, text)
	if err != nil {
		t.Fatalf("Parse(%q) error: %s", text, err)
	}
	op, err := ev.Compile(n, name, text)
	if err != nil {
		t.Fatalf("Compile(Parse(%q)) error: %s", text, err)
	}
	return op
}

func TestEval(t *testing.T) {
	util.InTempDir(func(string) {
		for _, filename := range filesToCreate {
			file, err := os.Create(filename)
			if err != nil {
				panic(err)
			}
			file.Close()
		}
		for _, tt := range evalTests {
			// fmt.Printf("eval %q\n", tt.text)

			out, bytesOut, err := evalAndCollect(t, []string{tt.text}, len(tt.want.out))

			first := true
			errorf := func(format string, args ...interface{}) {
				if first {
					first = false
					t.Errorf("eval(%q) fails:", tt.text)
				}
				t.Errorf("  "+format, args...)
			}

			if !matchOut(tt.want.out, out) {
				errorf("got out=%v, want %v", out, tt.want.out)
			}
			if string(tt.want.bytesOut) != string(bytesOut) {
				errorf("got bytesOut=%q, want %q", bytesOut, tt.want.bytesOut)
			}
			if !matchErr(tt.want.err, err) {
				errorf("got err=%v, want %v", err, tt.want.err)
			}
		}
	})
}

func TestMultipleEval(t *testing.T) {
	texts := []string{"x=hello", "put $x"}
	outs, _, err := evalAndCollect(t, texts, 1)
	wanted := strs("hello")
	if err != nil {
		t.Errorf("eval %s => %v, want nil", texts, err)
	}
	if !reflect.DeepEqual(outs, wanted) {
		t.Errorf("eval %s outputs %v, want %v", texts, outs, wanted)
	}
}

func evalAndCollect(t *testing.T, texts []string, chsize int) ([]Value, []byte, error) {
	name := "<eval test>"
	ev := NewEvaler(api.NewClient("/invalid"), nil, dataDir, nil)

	// Collect byte output
	bytesOut := []byte{}
	pr, pw, _ := os.Pipe()
	bytesDone := make(chan struct{})
	go func() {
		for {
			var buf [64]byte
			nr, err := pr.Read(buf[:])
			bytesOut = append(bytesOut, buf[:nr]...)
			if err != nil {
				break
			}
		}
		close(bytesDone)
	}()

	// Channel output
	outs := []Value{}

	// Eval error. Only that of the last text is saved.
	var ex error

	for _, text := range texts {
		op := mustParseAndCompile(t, ev, name, text)

		outCh := make(chan Value, chsize)
		outDone := make(chan struct{})
		go func() {
			for v := range outCh {
				outs = append(outs, v)
			}
			close(outDone)
		}()

		ports := []*Port{
			{File: os.Stdin, Chan: ClosedChan},
			{File: pw, Chan: outCh},
			{File: os.Stderr, Chan: BlackholeChan},
		}

		ex = ev.eval(op, ports, name, text)
		close(outCh)
		<-outDone
	}

	pw.Close()
	<-bytesDone
	pr.Close()

	return outs, bytesOut, ex
}

func matchOut(want, got []Value) bool {
	if len(got) == 0 && len(want) == 0 {
		return true
	}
	return reflect.DeepEqual(got, want)
}

func matchErr(want, got error) bool {
	if got == nil {
		return want == nil
	}
	return want == errAny || reflect.DeepEqual(got.(*Exception).Cause, want)
}

func BenchmarkOutputCaptureOverhead(b *testing.B) {
	op := Op{func(*EvalCtx) {}, 0, 0}
	benchmarkOutputCapture(op, b.N)
}

func BenchmarkOutputCaptureValues(b *testing.B) {
	op := Op{func(ec *EvalCtx) {
		ec.ports[1].Chan <- String("test")
	}, 0, 0}
	benchmarkOutputCapture(op, b.N)
}

func BenchmarkOutputCaptureBytes(b *testing.B) {
	bytesToWrite := []byte("test")
	op := Op{func(ec *EvalCtx) {
		ec.ports[1].File.Write(bytesToWrite)
	}, 0, 0}
	benchmarkOutputCapture(op, b.N)
}

func BenchmarkOutputCaptureMixed(b *testing.B) {
	bytesToWrite := []byte("test")
	op := Op{func(ec *EvalCtx) {
		ec.ports[1].Chan <- Bool(false)
		ec.ports[1].File.Write(bytesToWrite)
	}, 0, 0}
	benchmarkOutputCapture(op, b.N)
}

func benchmarkOutputCapture(op Op, n int) {
	ev := NewEvaler(api.NewClient("/invalid"), nil, "", nil)
	ec := NewTopEvalCtx(ev, "[benchmark]", "", []*Port{{}, {}, {}})
	for i := 0; i < n; i++ {
		pcaptureOutput(ec, op)
	}
}
