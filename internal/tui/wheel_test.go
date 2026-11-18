package tui

import "testing"

func scrollLines(a *wheelScrollAccelerator, times []int64, direction int) []int {
	out := make([]int, len(times))
	for i, now := range times {
		out[i] = a.next(direction, now)
	}
	return out
}

func TestWheelScrollFixedIgnoresTiming(t *testing.T) {
	a := newWheelScrollAccelerator(3, true)
	got := scrollLines(a, []int64{0, 10, 20, 1000}, 1)
	want := []int{3, 3, 3, 3}
	if !intsEqual(got, want) {
		t.Fatalf("fixed = %v, want %v", got, want)
	}
	a.setLines(0.5)
	if n := a.next(1, 2000); n != 1 {
		t.Fatalf("0.5 = %d", n)
	}
}

func TestWheelScrollAutoSkipsWhenTerminalAccelerates(t *testing.T) {
	a := newWheelScrollAccelerator("auto", false)
	got := scrollLines(a, []int64{0, 10, 20, 30}, 1)
	want := []int{1, 1, 1, 1}
	if !intsEqual(got, want) {
		t.Fatalf("accelerated terminal = %v", got)
	}
}

func TestWheelScrollAutoScalesWithVelocity(t *testing.T) {
	a := newWheelScrollAccelerator("auto", true)
	cases := []struct {
		times []int64
		want  []int
	}{
		{[]int64{0, 150, 300, 450}, []int{1, 1, 1, 1}},
		{[]int64{1000, 1050, 1100, 1150}, []int{1, 2, 2, 2}},
		{[]int64{2000, 2020, 2040, 2060}, []int{1, 5, 5, 5}},
		{[]int64{3000, 3010, 3020, 3030}, []int{1, 6, 6, 6}},
	}
	for _, tc := range cases {
		got := scrollLines(a, tc.times, 1)
		if !intsEqual(got, tc.want) {
			t.Fatalf("times %v = %v, want %v", tc.times, got, tc.want)
		}
	}
}

func TestWheelScrollBurstDoesNotAccelerate(t *testing.T) {
	a := newWheelScrollAccelerator("auto", true)
	got := scrollLines(a, []int64{0, 3, 6, 9}, 1)
	want := []int{1, 1, 1, 1}
	if !intsEqual(got, want) {
		t.Fatalf("burst = %v", got)
	}
}

func TestWheelScrollResetsOnDirectionAndPause(t *testing.T) {
	a := newWheelScrollAccelerator("auto", true)
	got := scrollLines(a, []int64{0, 20, 40}, 1)
	if !intsEqual(got, []int{1, 5, 5}) {
		t.Fatalf("spin = %v", got)
	}
	if n := a.next(-1, 60); n != 1 {
		t.Fatalf("reverse = %d", n)
	}
	got = scrollLines(a, []int64{500, 520}, 1)
	if !intsEqual(got, []int{1, 5}) {
		t.Fatalf("after pause = %v", got)
	}
}

func TestWheelScrollCarriesFraction(t *testing.T) {
	a := newWheelScrollAccelerator("auto", true)
	got := scrollLines(a, []int64{0, 40, 80, 120, 160}, 1)
	want := []int{1, 2, 3, 2, 3}
	if !intsEqual(got, want) {
		t.Fatalf("carry = %v, want %v", got, want)
	}
}

func TestTerminalAcceleratesWheelOnlyOnLocalDarwin(t *testing.T) {
	if wheelAlreadyAccelerated("linux", "", "", "") {
		t.Fatal("linux should accelerate in auto mode")
	}
	if !wheelAlreadyAccelerated("darwin", "", "", "") {
		t.Fatal("local darwin should keep one line")
	}
	if wheelAlreadyAccelerated("darwin", "1.2.3.4 5 6", "", "") {
		t.Fatal("ssh connection should accelerate")
	}
	if wheelAlreadyAccelerated("darwin", "", "1.2.3.4 5", "") {
		t.Fatal("ssh client should accelerate")
	}
	if wheelAlreadyAccelerated("darwin", "", "", "/dev/pts/1") {
		t.Fatal("ssh tty should accelerate")
	}
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
