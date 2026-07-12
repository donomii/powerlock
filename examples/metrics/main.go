package main

import (
	"bytes"
	"fmt"
	"strings"

	powerlockprometheus "github.com/donomii/powerlock/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

func main() {
	registry := prometheus.NewRegistry()
	observer := powerlockprometheus.MustNew(registry)
	lock := observer.NewObservedRWMutex("cache-update")

	lock.Lock()
	families, err := registry.Gather()
	if err != nil {
		panic(err)
	}
	lock.Unlock()

	var output bytes.Buffer
	encoder := expfmt.NewEncoder(&output, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, family := range families {
		if strings.HasPrefix(family.GetName(), "powerlock_") {
			if err := encoder.Encode(family); err != nil {
				panic(err)
			}
		}
	}

	fmt.Print(output.String())
}
