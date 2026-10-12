package resourcemanager

import "testing"

func BenchmarkRequestRelease(b *testing.B) {
	m := New[int](1 << 30)
	defer m.Close()
	notify := make(chan int, 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !m.Request("t", 1, 1<<20, notify, nil) {
			b.Fatal("request not granted")
		}
		m.Release(1 << 20)
	}
}
