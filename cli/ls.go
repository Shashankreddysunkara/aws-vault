package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/99designs/aws-vault/vault"
	"github.com/99designs/keyring"
	"gopkg.in/alecthomas/kingpin.v2"
)

type LsCommandInput struct {
	Keyring         keyring.Keyring
	OnlyProfiles    bool
	OnlySessions    bool
	OnlyCredentials bool
}

func ConfigureListCommand(app *kingpin.Application) {
	input := LsCommandInput{}

	cmd := app.Command("list", "List profiles, along with their credentials and sessions")
	cmd.Alias("ls")

	cmd.Flag("profiles", "Show only the profile names").
		BoolVar(&input.OnlyProfiles)

	cmd.Flag("sessions", "Show only the session names").
		BoolVar(&input.OnlySessions)

	cmd.Flag("credentials", "Show only the credential names").
		BoolVar(&input.OnlyCredentials)

	cmd.Action(func(c *kingpin.ParseContext) error {
		input.Keyring = keyringImpl
		LsCommand(app, input)
		return nil
	})
}

func contains(profileName string, credentialNames []string) bool {
	for _, credentialName := range credentialNames {
		if !vault.IsSessionKey(credentialName) && credentialName == profileName {
			return true
		}
	}
	return false
}

func LsCommand(app *kingpin.Application, input LsCommandInput) {
	krs, err := vault.NewKeyringSessions(input.Keyring, awsConfig)
	if err != nil {
		app.Fatalf(err.Error())
		return
	}

	credentialNames, err := input.Keyring.Keys()
	if err != nil {
		app.Fatalf(err.Error())
		return
	}

	if input.OnlyCredentials {
		for _, c := range credentialNames {
			if !vault.IsSessionKey(c) {
				fmt.Printf("%s\n", c)
			}
		}
		return
	}

	if input.OnlyProfiles {
		for _, profile := range awsConfig.Profiles() {
			fmt.Printf("%s\n", profile.Name)
		}
		return
	}

	if input.OnlySessions {
		for _, c := range credentialNames {
			if vault.IsSessionKey(c) {
				fmt.Printf("%s\n", c)
			}
		}
		return
	}

	sessions, err := krs.Sessions()
	if err != nil {
		app.Fatalf(err.Error())
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 25, 4, 2, ' ', 0)
	fmt.Fprintln(w, "Profile\tCredentials\tSessions\t")
	fmt.Fprintln(w, "=======\t===========\t========\t")

	// list out known profiles first
	for _, profile := range awsConfig.Profiles() {
		fmt.Fprintf(w, "%s\t", profile.Name)

		source, _ := awsConfig.SourceProfile(profile.Name)
		if contains(source.Name, credentialNames) {
			fmt.Fprintf(w, "%s\t", source.Name)
		} else {
			fmt.Fprintf(w, "-\t")
		}

		var sessionLabels []string
		for _, sess := range sessions {
			if profile.Name == sess.Profile.Name {
				label := fmt.Sprintf("%d", sess.Expiration.Unix())
				if sess.MfaSerial != "" {
					label += " (mfa)"
				}
				sessionLabels = append(sessionLabels, label)
			}
		}

		if len(sessions) > 0 {
			fmt.Fprintf(w, "%s\t\n", strings.Join(sessionLabels, ", "))
		} else {
			fmt.Fprintf(w, "-\t\n")
		}
	}

	// show credentials that don't have profiles
	for _, c := range credentialNames {
		if !vault.IsSessionKey(c) {
			if _, ok := awsConfig.Profile(c); !ok {
				fmt.Fprintf(w, "-\t%s\t-\t\n", c)
			}
		}
	}

	if err = w.Flush(); err != nil {
		app.Fatalf("%v", err)
		return
	}

	if len(credentialNames) == 0 {
		app.Fatalf("No credentials found")
		return
	}
}
