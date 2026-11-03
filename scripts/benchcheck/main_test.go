package main

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	in := `goos: linux
goarch: amd64
pkg: github.com/hipjim/nib
cpu: Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz
,old.txt,,new.txt,,,
,sec/op,CI,sec/op,CI,vs base,P
IndexBig-4,0.1,1%,0.125,1%,+25.00%,p=0.002 n=6
FindBig/zqxjv-4,0.01,1%,0.0105,1%,~,p=0.180 n=6
SaveBig-4,0.2,1%,0.19,1%,-5.00%,p=0.002 n=6
TypeInBig-4,4e-08,1%,4.3e-08,1%,+7.50%,p=0.002 n=6
NewOne-4,,,0.5,1%,,
geomean,0.05,,0.055,,+9.00%,

,old.txt,,new.txt,,,
,B/s,CI,B/s,CI,vs base,P
VisColLong-4,4e+09,1%,3e+09,1%,-25.00%,p=0.002 n=6
geomean,4e+09,,3e+09,,-25.00%,
`
	got, err := check(strings.NewReader(in), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "IndexBig-4: +25.00%" {
		t.Fatalf("slower: %q", got)
	}
	if got, _ := check(strings.NewReader(in), 5); len(got) != 2 {
		t.Fatalf("at 5%%: %q", got)
	}
}
