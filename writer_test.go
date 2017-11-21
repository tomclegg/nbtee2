package nbtee2

import (
	"io/ioutil"
	"sync"
	"testing"

	check "gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type Suite struct{}

var _ = check.Suite(&Suite{})

func (s *Suite) TestSmallBuffer(c *check.C) {
	w := &Tee{}
	r0 := w.NewReader(0)
	r1 := w.NewReader(1)
	r2 := w.NewReader(2)
	w.Write([]byte{1, 2, 3})
	w.Write([]byte{4, 5, 6})
	w.Write([]byte{7, 8, 9})
	w.Close()
	buf0, _ := ioutil.ReadAll(r0)
	buf1, _ := ioutil.ReadAll(r1)
	buf2, _ := ioutil.ReadAll(r2)
	c.Check(buf0, check.DeepEquals, []byte{})
	c.Check(buf1, check.DeepEquals, []byte{1, 2, 3})
	c.Check(buf2, check.DeepEquals, []byte{1, 2, 3})
}

func (s *Suite) TestWriter(c *check.C) {
	w := &Tee{}
	var wg sync.WaitGroup
	for i := 0; i < 256; i++ {
		w.Write([]byte{byte(i), byte(i) + 1, byte(i) + 2})
		r := w.NewReader(256)
		if i%7 == 3 {
			go func() {
				defer r.Close()
				wg.Wait()
			}()
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer r.Close()
			buf, err := ioutil.ReadAll(r)
			c.Check(err, check.Equals, nil)
			c.Check(len(buf), check.Equals, 3*(255-i))
		}(i)
	}
	c.Check(w.Close(), check.IsNil)
	wg.Wait()
}
