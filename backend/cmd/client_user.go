package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

var localUsername, localDisplayName, localEmail, localRole, localTokenName, localExpiresIn, localSecretFile string
var principalRepairConnectionID, principalRepairGitHubUserID, principalRepairLogin, principalRepairAvatarURL string
var principalRepairClearApplicationUserID, principalRepairReassignIdentity bool

var userCmd = &cobra.Command{Use: "user", Short: "Manage users"}
var userDeletePrincipalCmd = &cobra.Command{Use: "delete-principal <principal-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	return c.DeletePrincipal(cmd.Context(), args[0])
}}
var userRepairPrincipalCmd = &cobra.Command{Use: "repair-principal <principal-id>", Short: "Repair a principal's application user and GitHub identity links", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	if !principalRepairClearApplicationUserID && principalRepairConnectionID == "" && principalRepairGitHubUserID == "" {
		return errors.New("at least one repair operation is required")
	}
	request := &client.RepairPrincipalRequest{ClearApplicationUserID: principalRepairClearApplicationUserID}
	if principalRepairConnectionID != "" || principalRepairGitHubUserID != "" {
		if principalRepairConnectionID == "" || principalRepairGitHubUserID == "" {
			return errors.New("--identity-connection-id and --identity-github-user-id must be specified together")
		}
		githubUserID, err := strconv.ParseInt(principalRepairGitHubUserID, 10, 64)
		if err != nil || githubUserID <= 0 {
			return errors.New("--identity-github-user-id must be a positive integer")
		}
		request.Identities = []client.PrincipalRepairIdentity{{ConnectionID: principalRepairConnectionID, GitHubUserID: githubUserID, Login: principalRepairLogin, AvatarURL: principalRepairAvatarURL, ReassignExisting: principalRepairReassignIdentity}}
	}
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	out, err := c.RepairPrincipal(cmd.Context(), args[0], request)
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
}}
var userCreateCmd = &cobra.Command{Use: "create", RunE: func(cmd *cobra.Command, _ []string) error {
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	out, err := c.CreateLocalUser(context.Background(), &client.CreateLocalUserRequest{Username: localUsername, DisplayName: localDisplayName, Email: localEmail, Role: localRole})
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
}}
var userGetCmd = &cobra.Command{Use: "get <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	out, err := c.GetLocalUser(context.Background(), args[0])
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
}}
var userTokenCmd = &cobra.Command{Use: "token", Short: "Manage a user's API tokens"}
var userTokenCreateCmd = &cobra.Command{Use: "create <user-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	if localSecretFile == "" {
		return errors.New("--secret-file is required")
	}
	f, err := os.OpenFile(localSecretFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("create secret file: %w", err)
	}
	path := localSecretFile
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	out, err := c.CreateLocalUserToken(context.Background(), args[0], &client.CreateLocalUserTokenRequest{Name: localTokenName, ExpiresIn: localExpiresIn})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, out.PlaintextToken); err != nil {
		return fmt.Errorf("write secret file for issued token %s: %w", out.Token.ID, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close secret file for issued token %s: %w", out.Token.ID, err)
	}
	ok = true
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out.Token)
}}
var userTokenListCmd = &cobra.Command{Use: "list <user-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	out, err := c.ListLocalUserTokens(context.Background(), args[0])
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
}}
var userTokenRevokeCmd = &cobra.Command{Use: "revoke <user-id> <token-id>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	c, err := resolveBaseClient()
	if err != nil {
		return err
	}
	return c.DeleteLocalUserToken(context.Background(), args[0], args[1])
}}

func init() {
	userRepairPrincipalCmd.Flags().BoolVar(&principalRepairClearApplicationUserID, "clear-application-user-id", false, "clear the compatibility application user ID")
	userRepairPrincipalCmd.Flags().StringVar(&principalRepairConnectionID, "identity-connection-id", "", "GitHub connection ID for an identity to attach")
	userRepairPrincipalCmd.Flags().StringVar(&principalRepairGitHubUserID, "identity-github-user-id", "", "numeric GitHub user ID for an identity to attach")
	userRepairPrincipalCmd.Flags().StringVar(&principalRepairLogin, "identity-login", "", "GitHub login recorded on a newly attached identity")
	userRepairPrincipalCmd.Flags().StringVar(&principalRepairAvatarURL, "identity-avatar-url", "", "GitHub avatar URL recorded on a newly attached identity")
	userRepairPrincipalCmd.Flags().BoolVar(&principalRepairReassignIdentity, "reassign-identity", false, "move an existing identity from another principal")
	userCreateCmd.Flags().StringVar(&localUsername, "username", "", "Username (required)")
	_ = userCreateCmd.MarkFlagRequired("username")
	userCreateCmd.Flags().StringVar(&localDisplayName, "display-name", "", "Display name")
	userCreateCmd.Flags().StringVar(&localEmail, "email", "", "Email")
	userCreateCmd.Flags().StringVar(&localRole, "role", "user", "Role: user or admin")
	userTokenCreateCmd.Flags().StringVar(&localTokenName, "name", "", "Token name (required)")
	_ = userTokenCreateCmd.MarkFlagRequired("name")
	userTokenCreateCmd.Flags().StringVar(&localExpiresIn, "expires-in", "720h", "Token lifetime")
	userTokenCreateCmd.Flags().StringVar(&localSecretFile, "secret-file", "", "New file for the token secret (required)")
	userTokenCmd.AddCommand(userTokenCreateCmd, userTokenListCmd, userTokenRevokeCmd)
	userCmd.AddCommand(userCreateCmd, userGetCmd, userDeletePrincipalCmd, userRepairPrincipalCmd, userTokenCmd)
	ClientCmd.AddCommand(userCmd)
}
