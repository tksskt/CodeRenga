package shell

import "os"

func init() { getenv = os.Getenv }
