package consul

import "time"

type cache struct {
	Records map[string]Record
	Updated int64
}

// UpdateCache adds or removes a record from the cache. The key
// is the check or service ID
func (c *cache) UpdateCache(record Record, action string) {
	if c.Records == nil {
		c.Records = make(map[string]Record)
	}

	var id string
	switch record.Type {
	case "service":
		id = record.Service.ID
	case "check":
		id = record.Check.ID
	}

	switch action {
	case "add":
		c.Records[id] = record
	case "remove":
		delete(c.Records, id)
	}
	c.Updated = time.Now().UnixNano()
}

// CachedRecords returns a list of records that have been cached
func (c cache) CachedRecords() []Record {
	recs := make([]Record, len(c.Records))

	for _, record := range c.Records {
		recs = append(recs, record)
	}

	return recs
}
