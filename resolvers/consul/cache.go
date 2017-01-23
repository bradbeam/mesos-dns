package consul

import "time"

type cache struct {
	Records []Record
	Updated time.Time
}

func (c *cache) updateCache(record Record, action string) {
	switch action {
	case "add":
		c.Records = append(c.Records, record)
	case "remove":
		for index, rec := range c.Records {
			if record.Type != rec.Type {
				continue
			}

			switch record.Type {
			case "service":
				if rec.Service.ID == record.Service.ID {
					// Weird-ish way of deleting an item from a slice
					// we'll create a new slice and append all the
					// elements with an index > element we remove
					c.Records = append(c.Records[:index], c.Records[index+1:]...)
					c.Updated = time.Now()
					break
				}
			case "check":
				if rec.Check.ID == record.Check.ID {
					c.Records = append(c.Records[:index], c.Records[index+1:]...)
					c.Updated = time.Now()
					break
				}
			}
		}
	}

}
