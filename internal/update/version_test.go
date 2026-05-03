package update

import "testing"

func TestIsDevBuild(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"dev", true},
		{"unknown", true},
		{"0.4.1-next", true},
		{"0.4.0-next-abcdef", true},
		{"0.4.0-dirty", true},
		{"0.4.0", false},
		{"v0.4.0", false},
		{"0.4.0-rc.1", false},
	}
	for _, c := range cases {
		if got := IsDevBuild(c.in); got != c.want {
			t.Errorf("IsDevBuild(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.3.1", -1},
		{"0.3.1", "0.3.0", 1},
		{"0.4.0", "0.4.0", 0},
		{"v0.4.0", "0.4.0", 0},
		{"0.4.0-rc.1", "0.4.0", -1},
		{"0.4.0", "0.4.0-rc.1", 1},
		{"0.4.0-rc.1", "0.4.0-rc.2", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1},
		{"0.4.0+build.1", "0.4.0", 0}, // build metadata 무시
		// 잘못된 입력은 보수적으로 0 반환
		{"garbage", "0.4.0", 0},
		{"0.4.0", "", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestStripV(t *testing.T) {
	if StripV("v1.2.3") != "1.2.3" {
		t.Fail()
	}
	if StripV("1.2.3") != "1.2.3" {
		t.Fail()
	}
	if StripV("  v1.2.3 ") != "1.2.3" {
		t.Fail()
	}
}
