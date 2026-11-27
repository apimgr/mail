//go:build windows

package server

import "os"

func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func operationalSignals() []os.Signal {
	return nil
}

func ignoredSignals() []os.Signal {
	return nil
}

func operationalSignalAction(os.Signal) string {
	return ""
}
