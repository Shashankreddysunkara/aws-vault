package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/99designs/aws-vault/prompt"
	"github.com/99designs/aws-vault/server"
	"github.com/99designs/aws-vault/vault"
	"github.com/99designs/keyring"
	"gopkg.in/alecthomas/kingpin.v2"
)

type ExecCommandInput struct {
	ProfileName      string
	Command          string
	Args             []string
	Keyring          keyring.Keyring
	StartServer      bool
	CredentialHelper bool
	Signals          chan os.Signal
	Config           vault.Config
}

type AwsConfigOverride struct {
	MfaSerial string
}

// json metadata for AWS credential process. Ref: https://docs.aws.amazon.com/cli/latest/topic/config-vars.html#sourcing-credentials-from-external-processes
type AwsCredentialHelperData struct {
	Version         int    `json:"Version"`
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken"`
	Expiration      string `json:"Expiration,omitempty"`
}

func ConfigureExecCommand(app *kingpin.Application) {
	input := ExecCommandInput{}

	cmd := app.Command("exec", "Executes a command with AWS credentials in the environment")
	cmd.Flag("no-session", "Use root credentials, no session created").
		Short('n').
		BoolVar(&input.Config.NoSession)

	cmd.Flag("session-ttl", "Expiration time for aws session").
		Default("4h").
		Envar("AWS_SESSION_TTL").
		Short('t').
		DurationVar(&input.Config.SessionDuration)

	cmd.Flag("assume-role-ttl", "Expiration time for aws assumed role").
		Default("15m").
		Envar("AWS_ASSUME_ROLE_TTL").
		DurationVar(&input.Config.AssumeRoleDuration)

	cmd.Flag("mfa-token", "The mfa token to use").
		Short('m').
		StringVar(&input.Config.MfaToken)

	cmd.Flag("mfa-serial-override", "Deprecated, use --mfa-serial instead").
		Hidden().
		StringVar(&input.Config.MfaSerial)

	cmd.Flag("mfa-serial", "The identification number of the MFA device to use").
		// Envar("AWS_MFA_SERIAL").
		StringVar(&input.Config.MfaSerial)

	cmd.Flag("json", "AWS credential helper. Ref: https://docs.aws.amazon.com/cli/latest/topic/config-vars.html#sourcing-credentials-from-external-processes").
		Short('j').
		BoolVar(&input.CredentialHelper)

	cmd.Flag("server", "Run the server in the background for credentials").
		Short('s').
		BoolVar(&input.StartServer)

	cmd.Arg("profile", "Name of the profile").
		Required().
		HintAction(awsConfigFile.ProfileNames).
		StringVar(&input.ProfileName)

	cmd.Arg("cmd", "Command to execute").
		Default(os.Getenv("SHELL")).
		StringVar(&input.Command)

	cmd.Arg("args", "Command arguments").
		StringsVar(&input.Args)

	cmd.Action(func(c *kingpin.ParseContext) error {
		input.Keyring = keyringImpl
		input.Config.MfaPrompt = prompt.Method(GlobalFlags.PromptDriver)
		input.Signals = make(chan os.Signal)
		ExecCommand(app, input)
		return nil
	})
}

func ExecCommand(app *kingpin.Application, input ExecCommandInput) {
	if os.Getenv("AWS_VAULT") != "" {
		app.Fatalf("aws-vault sessions should be nested with care, unset $AWS_VAULT to force")
		return
	}

	var setEnv = true

	if input.Config.NoSession && input.StartServer {
		app.Fatalf("Can't start a credential server without a session")
		return
	}

	err := configLoader.LoadFromProfile(input.ProfileName, &input.Config)
	if err != nil {
		app.Fatalf("%v", err)
	}

	creds, err := vault.NewVaultCredentials(input.Keyring, input.ProfileName, &input.Config)
	if err != nil {
		app.Fatalf("%v", err)
	}

	val, err := creds.Get()
	if err != nil {
		app.Fatalf(awsConfigFile.FormatCredentialError(err, input.ProfileName))
	}

	if input.StartServer {
		if err := server.StartCredentialsServer(creds); err != nil {
			app.Fatalf("Failed to start credential server: %v", err)
		} else {
			setEnv = false
		}
	}

	if input.CredentialHelper {
		credentialData := AwsCredentialHelperData{
			Version:         1,
			AccessKeyID:     val.AccessKeyID,
			SecretAccessKey: val.SecretAccessKey,
			SessionToken:    val.SessionToken,
		}
		if !input.Config.NoSession {
			credsExprest, err := creds.ExpiresAt()
			if err != nil {
				app.Fatalf("Error getting credential expiration: %v", err)
			}
			credentialData.Expiration = credsExprest.Format("2006-01-02T15:04:05Z")
		}
		json, err := json.Marshal(&credentialData)
		if err != nil {
			app.Fatalf("Error creating credential json")
		}
		fmt.Printf(string(json))
	} else {

		env := environ(os.Environ())
		env.Set("AWS_VAULT", input.ProfileName)

		env.Unset("AWS_ACCESS_KEY_ID")
		env.Unset("AWS_SECRET_ACCESS_KEY")
		env.Unset("AWS_CREDENTIAL_FILE")
		env.Unset("AWS_DEFAULT_PROFILE")
		env.Unset("AWS_PROFILE")

		log.Printf("Setting subprocess env: AWS_DEFAULT_REGION=%s, AWS_REGION=%s", input.Config.Region, input.Config.Region)
		env.Set("AWS_DEFAULT_REGION", input.Config.Region)
		env.Set("AWS_REGION", input.Config.Region)

		if setEnv {
			log.Println("Setting subprocess env: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY")
			env.Set("AWS_ACCESS_KEY_ID", val.AccessKeyID)
			env.Set("AWS_SECRET_ACCESS_KEY", val.SecretAccessKey)

			if val.SessionToken != "" {
				log.Println("Setting subprocess env: AWS_SESSION_TOKEN, AWS_SECURITY_TOKEN")
				env.Set("AWS_SESSION_TOKEN", val.SessionToken)
				env.Set("AWS_SECURITY_TOKEN", val.SessionToken)
			}
		}

		cmd := exec.Command(input.Command, input.Args...)
		cmd.Env = env
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		signal.Notify(input.Signals, os.Interrupt, os.Kill)

		if err := cmd.Start(); err != nil {
			app.Fatalf("%v", err)
		}
		// wait for the command to finish
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- cmd.Wait()
			close(waitCh)
		}()

		for {
			select {
			case sig := <-input.Signals:
				if err = cmd.Process.Signal(sig); err != nil {
					app.Errorf("%v", err)
					break
				}
			case err := <-waitCh:
				var waitStatus syscall.WaitStatus
				if exitError, ok := err.(*exec.ExitError); ok {
					waitStatus = exitError.Sys().(syscall.WaitStatus)
					os.Exit(waitStatus.ExitStatus())
				}
				if err != nil {
					app.Fatalf("%v", err)
				}
				return
			}
		}
	}
}

// environ is a slice of strings representing the environment, in the form "key=value".
type environ []string

// Unset an environment variable by key
func (e *environ) Unset(key string) {
	for i := range *e {
		if strings.HasPrefix((*e)[i], key+"=") {
			(*e)[i] = (*e)[len(*e)-1]
			*e = (*e)[:len(*e)-1]
			break
		}
	}
}

// Set adds an environment variable, replacing any existing ones of the same key
func (e *environ) Set(key, val string) {
	e.Unset(key)
	*e = append(*e, key+"="+val)
}
