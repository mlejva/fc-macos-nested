package main

import (
	"fmt"

	"github.com/Code-Hex/vz/v3"
)

func main() {
	fmt.Println("Starting vz test...")

	// Check if nested virtualization is supported
	supported := vz.IsNestedVirtualizationSupported()
	fmt.Printf("Nested virtualization supported: %v\n", supported)

	fmt.Println("Done!")
}
