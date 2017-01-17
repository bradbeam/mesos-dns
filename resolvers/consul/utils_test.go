package consul_test

import "time"

func readErrorChan(errch chan error) error {
	select {
	case err := <-errch:
		return err
	case <-time.After(5 * time.Second):
		return nil
	}
}
