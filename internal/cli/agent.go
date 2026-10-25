package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/client"
	"github.com/Bunn-Burrow/sandctl/internal/config"
	"github.com/Bunn-Burrow/sandctl/internal/output"
)

func newAgentCommand() *cobra.Command {
	var (
		server  string
		file    string
		outputF string
		yes     bool
	)

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agents",
		Long: `Manage Agent resources in the Bunn Burrow control plane.

An Agent describes a sandboxed workload (image, model configuration, network
and resource settings). Agents are created from a YAML or JSON file matching
the control plane's Agent schema (apiVersion agentplatform.io/v1alpha1,
kind Agent).`,
		Example: `  sandctl agent list
  sandctl agent get my-agent
  sandctl agent create -f agent.yaml
  sandctl agent update -f agent.yaml my-agent
  sandctl agent delete my-agent --yes`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			resp, err := c.Get(cmd.Context(), "/v1/agents")
			if err != nil {
				return handleError(err)
			}
			defer resp.Body.Close()

			var list struct {
				Items []agentItem `json:"items"`
			}
			if err := client.DecodeJSON(resp, &list); err != nil {
				return handleError(err)
			}

			w := output.NewWriter(parseFormat(outputF))
			if outputF == "json" || outputF == "yaml" {
				return w.JSON(list.Items)
			}

			rows := make([][]string, 0, len(list.Items))
			for _, a := range list.Items {
				model := "-"
				if a.Spec.Model != nil {
					model = a.Spec.Model.Provider + "/" + a.Spec.Model.Model
				}
				rows = append(rows, []string{
					string(a.Metadata.Name),
					a.Spec.Image.Reference,
					model,
					func() string {
						if a.Metadata.CreatedAt != nil {
							return output.Time(*a.Metadata.CreatedAt)
						}
						return "-"
					}(),
				})
			}
			return w.Table([]string{"NAME", "IMAGE", "MODEL", "CREATED AT"}, rows)
		},
	}
	listCmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	listCmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	cmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get an agent by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			resp, err := c.Get(cmd.Context(), "/v1/agents/"+args[0])
			if err != nil {
				return handleError(err)
			}
			defer resp.Body.Close()

			var agent agentItem
			if err := client.DecodeJSON(resp, &agent); err != nil {
				return handleError(err)
			}

			w := output.NewWriter(parseFormat(outputF))
			if outputF == "json" || outputF == "yaml" {
				return w.JSON(agent)
			}

			fields := [][2]string{
				{"apiVersion", stringify(agent.ApiVersion)},
				{"kind", stringify(agent.Kind)},
				{"name", string(agent.Metadata.Name)},
				{"image", agent.Spec.Image.Reference},
			}
			if agent.Spec.Model != nil {
				fields = append(fields, [2]string{"model.provider", agent.Spec.Model.Provider})
				fields = append(fields, [2]string{"model.model", agent.Spec.Model.Model})
			}
			if agent.Metadata.CreatedAt != nil {
				fields = append(fields, [2]string{"created at", output.Time(*agent.Metadata.CreatedAt)})
			}
			return w.Object(fields)
		},
	}
	getCmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	getCmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	cmd.AddCommand(getCmd)

	createCmd := &cobra.Command{
		Use:   "create -f <file>",
		Short: "Create an agent from a file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			body, err := loadBody(file)
			if err != nil {
				return err
			}

			var raw any
			if err := json.Unmarshal(body, &raw); err != nil {
				return fmt.Errorf("parse request body: %w", err)
			}

			resp, err := c.Post(cmd.Context(), "/v1/agents", raw)
			if err != nil {
				return handleError(err)
			}
			defer resp.Body.Close()

			var created agentItem
			if err := client.DecodeJSON(resp, &created); err != nil {
				return handleError(err)
			}

			w := output.NewWriter(parseFormat(outputF))
			if outputF == "json" || outputF == "yaml" {
				return w.JSON(created)
			}
			fields := [][2]string{
				{"name", string(created.Metadata.Name)},
				{"image", created.Spec.Image.Reference},
				{"status", "created"},
			}
			return w.Object(fields)
		},
	}
	createCmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	createCmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	createCmd.Flags().StringVarP(&file, "file", "f", "", "Path to agent YAML/JSON definition")
	createCmd.MarkFlagRequired("file")
	cmd.AddCommand(createCmd)

	updateCmd := &cobra.Command{
		Use:   "update -f <file> <name>",
		Short: "Replace an agent by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			body, err := loadBody(file)
			if err != nil {
				return err
			}
			var raw any
			if err := json.Unmarshal(body, &raw); err != nil {
				return fmt.Errorf("parse request body: %w", err)
			}

			resp, err := c.Put(cmd.Context(), "/v1/agents/"+args[0], raw)
			if err != nil {
				return handleError(err)
			}
			defer resp.Body.Close()

			var replaced agentItem
			if err := client.DecodeJSON(resp, &replaced); err != nil {
				return handleError(err)
			}

			w := output.NewWriter(parseFormat(outputF))
			if outputF == "json" || outputF == "yaml" {
				return w.JSON(replaced)
			}
			fields := [][2]string{
				{"name", string(replaced.Metadata.Name)},
				{"image", replaced.Spec.Image.Reference},
				{"status", "replaced"},
			}
			return w.Object(fields)
		},
	}
	updateCmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	updateCmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	updateCmd.Flags().StringVarP(&file, "file", "f", "", "Path to agent YAML/JSON definition")
	updateCmd.MarkFlagRequired("file")
	cmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an agent by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirmDestructive(yes, fmt.Sprintf("delete agent %q", args[0])); err != nil {
				return err
			}
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			resp, err := c.Delete(cmd.Context(), "/v1/agents/"+args[0])
			if err != nil {
				return handleError(err)
			}
			defer resp.Body.Close()
			if err := client.DecodeJSON(resp, nil); err != nil {
				return handleError(err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Agent %q deleted.\n", args[0])
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	deleteCmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.AddCommand(deleteCmd)

	return cmd
}

type agentItem struct {
	ApiVersion interface{} `json:"apiVersion"`
	Kind       interface{} `json:"kind"`
	Metadata   struct {
		Name            string     `json:"name"`
		CreatedAt       *time.Time `json:"createdAt,omitempty"`
		ResourceVersion *string    `json:"resourceVersion,omitempty"`
	} `json:"metadata"`
	Spec struct {
		Description *string `json:"description,omitempty"`
		DisplayName *string `json:"displayName,omitempty"`
		Image       struct {
			Reference string  `json:"reference"`
			Digest    *string `json:"digest,omitempty"`
		} `json:"image"`
		Model *struct {
			Provider      string `json:"provider"`
			Model         string `json:"model"`
			CredentialRef *struct {
				Name string `json:"name"`
			} `json:"credentialRef,omitempty"`
		} `json:"model,omitempty"`
	} `json:"spec"`
}

func stringify(v interface{}) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func parseFormat(f string) output.Format {
	switch strings.ToLower(f) {
	case "json":
		return output.FormatJSON
	case "yaml":
		return output.FormatYAML
	default:
		return output.FormatTable
	}
}
