package bind

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/mesosphere/mesos-dns/records"
)

type FileResolver struct {
	File *os.File
}

func New(config Config, errch chan<- error, version string) *FileResolver {
	// Assume our config has all the necessary stuff in it
	f, err := ioutil.TempFile("./", "mesos-dns")

	if err != nil {
		errch <- err
	}

	return &FileResolver{
		File: f,
	}

}

func (f *FileResolver) Reload(rg *records.RecordGenerator) {
	w := bufio.NewWriter(f.File)
	for record, ip := range rg.As {
		for _, addr := range ip {
			_, err := w.WriteString(fmt.Sprintf("%s\tIN\tA\t%s\n", addr, record))
			if err != nil {
				log.Println(err)
			}
		}
	}
	w.Flush()
	for record, ip := range rg.SRVs {
		for _, addr := range ip {
			_, err := w.WriteString(fmt.Sprintf("%s\tIN\tSRV\t%s\n", addr, record))
			if err != nil {
				log.Println(err)
			}
		}
	}
	w.Flush()
}
