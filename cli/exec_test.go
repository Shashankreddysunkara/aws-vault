package cli

import (
	kingpin "gopkg.in/alecthomas/kingpin.v2"

	"github.com/99designs/keyring"
)

func ExampleExecCommand() {
	keyringImpl = keyring.NewArrayKeyring([]keyring.Item{
		{Key: "llamas", Data: []byte(`{"AccessKeyID":"ABC","SecretAccessKey":"XYZ"}`)},
	})

	app := kingpin.New("aws-vault", "")
	ConfigureGlobals(app)
	ConfigureExecCommand(app)
	kingpin.MustParse(app.Parse([]string{
		"--debug", "exec", "--no-session", "llamas", "--", "sh", "-c", "echo $AWS_ACCESS_KEY_ID",
	}))

	// Output:
	// ABC
}
