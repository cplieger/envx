package envx_test

import (
	"fmt"
	"os"
	"time"

	"github.com/cplieger/envx"
)

func Example() {
	os.Setenv("APP_LISTEN", ":9090")
	os.Setenv("APP_DEBUG", "yes")
	defer os.Unsetenv("APP_LISTEN")
	defer os.Unsetenv("APP_DEBUG")

	addr := envx.String("APP_LISTEN", ":8080")
	debug := envx.Bool("APP_DEBUG", false)
	interval := envx.Duration("APP_INTERVAL", 6*time.Hour)

	fmt.Println(addr, debug, interval)
	// Output: :9090 true 6h0m0s
}

func ExampleRequire() {
	// Collect every missing variable, then fail once.
	var missing []error
	if _, err := envx.Require("APP_TOKEN_UNSET_A"); err != nil {
		missing = append(missing, err)
	}
	if _, err := envx.Require("APP_TOKEN_UNSET_B"); err != nil {
		missing = append(missing, err)
	}
	fmt.Println(len(missing), "missing")
	// Output: 2 missing
}
