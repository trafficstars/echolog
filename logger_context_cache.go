package echolog

import (
	"encoding/json"
	"fmt"
	"sync"

	labstacklog "github.com/labstack/gommon/log"
)

type cache struct {
	sync.RWMutex
	data []string
}

func (c *cache) Put(level labstacklog.Lvl, args ...interface{}) {
	if c == nil {
		return
	}
	c.Lock()
	defer c.Unlock()
	c.data = append(c.data, fmt.Sprint(args...))
}

func (c *cache) Putf(level labstacklog.Lvl, format string, args ...interface{}) {
	if c == nil {
		return
	}
	c.Lock()
	defer c.Unlock()
	c.data = append(c.data, fmt.Sprintf(format, args...))
}

func (c *cache) Putj(level labstacklog.Lvl, j labstacklog.JSON) {
	if c == nil {
		return
	}
	c.Lock()
	defer c.Unlock()
	if j, e := json.Marshal(j); e == nil {
		c.data = append(c.data, string(j))
	}
}

func (c *cache) Retrieve() []string {
	if c == nil {
		return nil
	}
	c.RLock()
	defer c.RUnlock()
	tmp := make([]string, len(c.data))
	copy(tmp, c.data)
	return tmp
}
