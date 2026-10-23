package hub

import (
	"strings"
	"testing"
)

func TestSplitFinalMessage(t *testing.T) {
	tests := []struct {
		name string
		text string
		mode string
		want []string
	}{
		{
			name: "no marker stays single",
			text: "一条完整回复",
			want: nil,
		},
		{
			name: "whole line marker splits",
			text: "收到！\n<msg-break>\n答案是 42。",
			mode: "public",
			want: []string{"收到！", "答案是 42。"},
		},
		{
			name: "marker mid sentence does not split",
			text: "这个 <msg-break> 在行内不算数",
			mode: "public",
			want: nil,
		},
		{
			name: "marker inside code fence is literal",
			text: "看这个例子：\n```\n<msg-break>\n```\n就长这样",
			mode: "public",
			want: nil,
		},
		{
			name: "empty segments are dropped",
			text: "第一段\n<msg-break>\n\n<msg-break>\n第二段",
			mode: "public",
			want: []string{"第一段", "第二段"},
		},
		{
			name: "all empty falls back to single",
			text: "<msg-break>\n<msg-break>",
			mode: "public",
			want: nil,
		},
		{
			name: "public burst capped at three with merged tail",
			text: "一\n<msg-break>\n二\n<msg-break>\n三\n<msg-break>\n四\n<msg-break>\n五",
			mode: "public",
			want: []string{"一", "二", "三\n\n四\n\n五"},
		},
		{
			name: "private burst capped at four",
			text: "一\n<msg-break>\n二\n<msg-break>\n三\n<msg-break>\n四\n<msg-break>\n五\n<msg-break>\n六",
			mode: "private",
			want: []string{"一", "二", "三", "四\n\n五\n\n六"},
		},
		{
			name: "fence toggling survives the split",
			text: "先发代码\n```go\nfmt.Println(1)\n```\n<msg-break>\n再解释",
			mode: "private",
			want: []string{"先发代码\n```go\nfmt.Println(1)\n```", "再解释"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitFinalMessage(tc.text, tc.mode)
			if len(got) != len(tc.want) {
				t.Fatalf("segments = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("segment %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// A split segment must never contain the raw marker: a stray marker in a
// stored message would render as literal text in the client.
func TestSplitFinalMessageNeverLeavesMarker(t *testing.T) {
	got := splitFinalMessage("a\n<msg-break>\nb\n<msg-break>\nc", "private")
	if len(got) != 3 {
		t.Fatalf("segments = %v", got)
	}
	for _, seg := range got {
		if strings.Contains(seg, finalSplitMarker) {
			t.Errorf("segment contains raw marker: %q", seg)
		}
	}
}
