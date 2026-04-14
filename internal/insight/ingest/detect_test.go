package ingest

import "testing"

func TestDetectKind(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{"foo.pdf", KindPDF},
		{"/abs/path/doc.PDF", KindPDF},
		{"note.md", KindMarkdown},
		{"note.markdown", KindMarkdown},
		{"note.txt", KindText},
		{"no-extension", KindText},
		{"https://www.youtube.com/watch?v=abc", KindYouTube},
		{"https://youtu.be/abc", KindYouTube},
		{"https://m.youtube.com/watch?v=abc", KindYouTube},
		{"https://en.wikipedia.org/wiki/X", KindURL},
		{"http://example.com", KindURL},
		{"", KindUnknown},
	}
	for _, c := range cases {
		got := DetectKind(c.in)
		if got != c.want {
			t.Errorf("DetectKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
