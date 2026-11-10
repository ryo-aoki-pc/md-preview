package htmltree

import (
	"context"
	"strings"
	"testing"
)

func TestJSONAndLower(t *testing.T) {
	for _, c := range [][2]string{{`{"2":1,"1":2,"b":1.0,"a":1e-7}`, `{"1":2,"2":1,"b":1,"a":1e-7}`}, {`{"x":1,"x":2}`, `{"x":2}`}, {`[1e-6,1e21,-0,1e400]`, `[0.000001,1e+21,0,null]`}, {`"\u2028"`, "\"\u2028\""}, {`"\\u2028"`, `"\\u2028"`}} {
		v, err := ParseJSON(c[0])
		if err != nil || JSON(v) != c[1] {
			t.Fatal(c, JSON(v), err)
		}
	}
	if Lower("İNI") != "i\u0307ni" {
		t.Fatal(Lower("İNI"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseJSONContext(ctx, `{"x":1}`); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestDepthAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fragment(ctx, "x"); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := HTMLContext(ctx, Root(Element("p", nil, Text("x")))); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := Fragment(context.Background(), strings.Repeat("<div>", 20000)); err != ErrDepth {
		t.Fatal(err)
	}
}
