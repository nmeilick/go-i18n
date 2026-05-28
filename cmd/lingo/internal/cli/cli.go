package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goformat "go/format"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nmeilick/go-i18n/extract"
	"github.com/nmeilick/go-i18n/gettext"
	"github.com/nmeilick/go-i18n/internal/atomicfile"
	"github.com/nmeilick/go-i18n/locale/cldr"
	"github.com/nmeilick/go-i18n/locale/cldrpack"
	"github.com/nmeilick/go-i18n/workflow"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

const AppName = "lingo"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type app struct {
	configPath string
	jsonOut    bool
	verbose    int
	stdout     io.Writer
	stderr     io.Writer
}

type configLocation struct {
	Path     string `json:"path"`
	Source   string `json:"source"`
	Exists   bool   `json:"exists"`
	Required bool   `json:"required,omitempty"`
}

// New creates the root command.
func New() *cobra.Command {
	return newCommand(os.Stdout, os.Stderr)
}

// Execute runs lingo and prints process-level errors to stderr.
func Execute(args []string, stdout, stderr io.Writer) int {
	cmd := newCommand(stdout, stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func newCommand(stdout, stderr io.Writer) *cobra.Command {
	a := &app{stdout: stdout, stderr: stderr}
	root := &cobra.Command{
		Use:               "lingo",
		Short:             "Manage gettext catalogs for Go i18n projects.",
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	}
	root.PersistentFlags().StringVarP(&a.configPath, "config", "C", "", "config file path (env: LINGO_CONFIG)")
	root.PersistentFlags().BoolVarP(&a.jsonOut, "json", "j", false, "write machine-readable JSON output")
	root.PersistentFlags().CountVarP(&a.verbose, "verbose", "v", "increase diagnostic verbosity")

	root.AddCommand(a.versionCmd())
	root.AddCommand(a.configCmd())
	root.AddCommand(a.extractCmd())
	root.AddCommand(a.updateCmd())
	root.AddCommand(a.checkCmd())
	root.AddCommand(a.dataCmd())
	root.AddCommand(a.formatCmd())
	root.AddCommand(a.staleCmd())
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	return root
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version metadata.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, map[string]any{"version": Version, "commit": Commit, "date": Date}, "lingo %s (%s, %s)\n", Version, Commit, Date)
		},
	}
}

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect and manage lingo configuration."}
	var pathAll bool
	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the active config path.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			locations := a.configLocations()
			if pathAll {
				if a.jsonOut {
					return a.writeJSON(cmd, map[string]any{"paths": locations})
				}
				for _, loc := range locations {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\texists=%t\n", loc.Source, loc.Path, loc.Exists); err != nil {
						return err
					}
				}
				return nil
			}
			loc := selectConfigLocation(locations)
			if a.jsonOut {
				return a.writeJSON(cmd, loc)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), loc.Path)
			return err
		},
	}
	pathCmd.Flags().BoolVar(&pathAll, "all", false, "show config search paths")
	cmd.AddCommand(pathCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print effective configuration.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.writeJSON(cmd, cfg)
			}
			data, err := toml.Marshal(cfg)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	})
	var initPath string
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default project config.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := initPath
			if path == "" {
				path = defaultProjectConfigPath()
			}
			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("config already exists at %s; use --force to overwrite", path)
				}
			}
			if err := workflow.WriteDefaultConfig(path); err != nil {
				return err
			}
			return a.write(cmd, map[string]any{"path": path, "created": true}, "created %s\n", path)
		},
	}
	initCmd.Flags().StringVar(&initPath, "path", "", "config path to create")
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing config")
	cmd.AddCommand(initCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate configuration without mutating files.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfig(true)
			if err != nil {
				return err
			}
			if _, err := workflow.New(cfg); err != nil {
				return err
			}
			return a.write(cmd, map[string]any{"path": path, "valid": true}, "valid %s\n", path)
		},
	})
	var updateDryRun bool
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Rewrite an existing config in the latest canonical format.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := a.loadConfig(true)
			if err != nil {
				return err
			}
			if _, err := workflow.New(cfg); err != nil {
				return err
			}
			data, err := toml.Marshal(cfg)
			if err != nil {
				return err
			}
			if updateDryRun {
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("config file %s does not exist; run lingo config init", path)
			}
			backup := path + ".bak"
			// #nosec G304 -- config update reads the caller-selected local config file.
			old, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := atomicfile.WriteFile(backup, old, 0o600); err != nil {
				return err
			}
			if err := atomicfile.WriteFile(path, data, 0o600); err != nil {
				return err
			}
			return a.write(cmd, map[string]any{"path": path, "backup": backup, "updated": true}, "updated %s (backup %s)\n", path, backup)
		},
	}
	updateCmd.Flags().BoolVarP(&updateDryRun, "dry-run", "n", false, "print updated config without writing files")
	cmd.AddCommand(updateCmd)
	var editor string
	editCmd := &cobra.Command{
		Use:   "edit",
		Short: "Open the user config in an editor.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loc := selectConfigLocation(a.configLocations())
			if !loc.Exists {
				return fmt.Errorf("config file %s does not exist; run lingo config init", loc.Path)
			}
			editorCmd := resolveEditor(editor)
			if editorCmd == "" {
				return errors.New("no editor found; set --editor, LINGO_EDITOR, VISUAL, or EDITOR")
			}
			parts := strings.Fields(editorCmd)
			// #nosec G204 -- config edit intentionally runs the operator-selected local editor.
			c := exec.Command(parts[0], append(parts[1:], loc.Path)...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("editor failed: %w", err)
			}
			return nil
		},
	}
	editCmd.Flags().StringVar(&editor, "editor", "", "editor command")
	cmd.AddCommand(editCmd)
	return cmd
}

func (a *app) extractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "extract",
		Short: "Extract messages and print POT to stdout.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			doc, report, err := w.Extract(context.Background())
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.writeJSON(cmd, report)
			}
			return gettext.WritePOT(cmd.OutOrStdout(), doc)
		},
	}
}

func (a *app) updateCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Extract and update POT/PO catalogs.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.Update(context.Background(), dryRun)
			if err != nil {
				return err
			}
			return a.write(cmd, report, "messages=%d warnings=%d changed=%d\n", report.Messages, report.Warnings, len(report.Changed))
		},
	}
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show what would change without writing files")
	return cmd
}

func (a *app) checkCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check catalogs without mutating files.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.Check(context.Background(), strict)
			if err != nil {
				return err
			}
			return a.write(cmd, report, "ok messages=%d warnings=%d\n", report.Messages, report.Warnings)
		},
	}
	cmd.Flags().BoolVarP(&strict, "strict", "s", false, "fail on strict catalog and extraction warnings")
	return cmd
}

func (a *app) formatCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "format",
		Short: "Rewrite configured catalogs deterministically.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.Format(context.Background(), dryRun)
			if err != nil {
				return err
			}
			return a.write(cmd, map[string]any{"changed": report.Changed, "dry_run": dryRun}, "formatted changed=%d\n", len(report.Changed))
		},
	}
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show what would change without writing files")
	return cmd
}

func (a *app) staleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stale",
		Short: "Report stale catalog state.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.Stale(context.Background())
			if err != nil {
				return err
			}
			return a.write(cmd, map[string]any{"stale": report.Stale, "messages": report.Messages}, "stale=%d messages=%d\n", len(report.Stale), report.Messages)
		},
	}
}

func (a *app) dataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Maintain generated CLDR data.",
	}
	cmd.AddCommand(a.dataBundlesCmd())
	cmd.AddCommand(&cobra.Command{
		Use:   "sources",
		Short: "Print generated-data source metadata.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.DataSources(context.Background())
			if err != nil {
				return err
			}
			return a.write(cmd, report, "cldr=%s unicode=%s source=%s output=%s\n", report.CLDRVersion, report.UnicodeVersion, report.SourceDir, report.OutputFile)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check generated CLDR data without writing files.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.DataCheck(context.Background())
			if err != nil {
				if a.jsonOut {
					_ = a.writeJSON(cmd, report)
				}
				return err
			}
			return a.write(cmd, report, "generated data ok cldr=%s bytes=%d\n", report.CLDRVersion, report.OutputBytes)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "diff",
		Short: "Show which generated CLDR files would change.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.DataDiff(context.Background())
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.writeJSON(cmd, report)
			}
			if len(report.Changed) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "generated data is current")
				return err
			}
			for _, path := range report.Changed {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "changed %s\n", path); err != nil {
					return err
				}
			}
			return nil
		},
	})
	var dryRun bool
	var force bool
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Regenerate CLDR data from locked local assets.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := a.loadConfig(false)
			if err != nil {
				return err
			}
			w, err := workflow.New(cfg)
			if err != nil {
				return err
			}
			report, err := w.DataUpdate(context.Background(), dryRun, force)
			if err != nil {
				if a.jsonOut {
					_ = a.writeJSON(cmd, report)
				}
				return err
			}
			return a.write(cmd, report, "generated data changed=%d bytes=%d\n", len(report.Changed), report.OutputBytes)
		},
	}
	updateCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show generated-data changes without writing files")
	updateCmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite generated files even when safety checks object")
	cmd.AddCommand(updateCmd)
	return cmd
}

type bundleRequest struct {
	name           string
	mode           string
	output         string
	packageName    string
	codec          string
	locales        []string
	languages      []string
	features       []string
	scanMode       string
	entrypoints    []string
	scanRoots      []string
	featureExtra   []string
	featureExclude []string
	featureRules   []string
	autoThreshold  int64
	moduleDir      string
	allowOutside   bool
}

type bundleReport struct {
	Version        int                        `json:"version"`
	Name           string                     `json:"name"`
	Mode           string                     `json:"mode"`
	Output         string                     `json:"output,omitempty"`
	Package        string                     `json:"package,omitempty"`
	Codec          string                     `json:"codec,omitempty"`
	Features       []cldr.FeatureID           `json:"features"`
	Locales        []string                   `json:"locales"`
	Closure        []string                   `json:"closure,omitempty"`
	Scan           *extract.FeatureScanReport `json:"scan,omitempty"`
	OutputBytes    int                        `json:"output_bytes,omitempty"`
	AutoThreshold  int64                      `json:"auto_threshold_bytes,omitempty"`
	DryRun         bool                       `json:"dry_run,omitempty"`
	SizeReport     map[string]int             `json:"size_report,omitempty"`
	HashVerified   bool                       `json:"hash_verified,omitempty"`
	SignatureCheck bool                       `json:"signature_check,omitempty"`
}

func (a *app) dataBundlesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bundles", Short: "Plan, generate, check, and describe CLDR bundles."}
	planReq := bundleRequest{}
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Expand bundle selectors without writing files.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := a.resolveBundleRequest(cmd, planReq)
			if err != nil {
				return err
			}
			report, err := planBundle(cmd.Context(), req)
			if err != nil {
				return err
			}
			return a.writeBundleReport(cmd, report)
		},
	}
	addBundleSelectionFlags(planCmd, &planReq)
	cmd.AddCommand(planCmd)

	genReq := bundleRequest{mode: "external-pack", codec: "raw"}
	var dryRun bool
	genCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a selected CLDR bundle.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := a.resolveBundleRequest(cmd, genReq)
			if err != nil {
				return err
			}
			report, err := generateBundle(cmd.Context(), req, dryRun)
			if err != nil {
				return err
			}
			return a.writeBundleReport(cmd, report)
		},
	}
	addBundleSelectionFlags(genCmd, &genReq)
	genCmd.Flags().StringVarP(&genReq.output, "output", "o", "", "output .cldrpack or generated .go path")
	genCmd.Flags().StringVar(&genReq.mode, "mode", "external-pack", "bundle mode: external-pack, embedded-pack, native-go, auto")
	genCmd.Flags().StringVar(&genReq.codec, "codec", "raw", "pack chunk codec: raw or zstd")
	genCmd.Flags().StringVar(&genReq.packageName, "package", "cldrbundle", "generated Go package name")
	genCmd.Flags().Int64Var(&genReq.autoThreshold, "auto-threshold-bytes", 512<<10, "deterministic auto-mode embedded-pack threshold")
	genCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "plan generation without writing files")
	cmd.AddCommand(genCmd)

	checkCmd := &cobra.Command{
		Use:   "check PACK",
		Short: "Validate a .cldrpack file and verify its whole-pack hash.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := cldrpack.Open(args[0], cldrpack.WithHashVerification(true))
			if err != nil {
				return err
			}
			defer bundle.Close()
			if err := cldrpack.Validate(bundle); err != nil {
				return err
			}
			stats, _ := cldrpack.BundleStats(bundle)
			report := reportFromBundle(bundle, args[0])
			report.HashVerified = stats.HashVerified
			return a.writeBundleReport(cmd, report)
		},
	}
	cmd.AddCommand(checkCmd)

	describeCmd := &cobra.Command{
		Use:   "describe PACK",
		Short: "Describe bundle metadata and coverage.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := cldrpack.Open(args[0])
			if err != nil {
				return err
			}
			defer bundle.Close()
			return a.writeBundleReport(cmd, reportFromBundle(bundle, args[0]))
		},
	}
	cmd.AddCommand(describeCmd)
	return cmd
}

func addBundleSelectionFlags(cmd *cobra.Command, req *bundleRequest) {
	cmd.Flags().StringVar(&req.name, "name", "custom", "bundle name")
	cmd.Flags().StringSliceVar(&req.locales, "locales", nil, "exact locales to include, or all")
	cmd.Flags().StringSliceVar(&req.languages, "languages", nil, "language families to include, or all")
	cmd.Flags().StringSliceVar(&req.features, "features", nil, "feature IDs, domains, auto, or all")
	cmd.Flags().StringVar(&req.scanMode, "scan-mode", "auto", "auto feature scan mode: auto, module, importers")
	cmd.Flags().StringSliceVar(&req.entrypoints, "entrypoints", nil, "main package patterns that seed auto feature scans")
	cmd.Flags().StringSliceVar(&req.scanRoots, "scan", nil, "package patterns or directories to scan instead of auto discovery")
	cmd.Flags().StringSliceVar(&req.featureExtra, "feature-extra", nil, "extra feature IDs or domains to include")
	cmd.Flags().StringSliceVar(&req.featureExclude, "feature-exclude", nil, "feature IDs or domains to exclude")
	cmd.Flags().StringArrayVar(&req.featureRules, "feature-rule", nil, "wrapper feature rule target=feature[,feature]")
}

func (a *app) resolveBundleRequest(cmd *cobra.Command, req bundleRequest) (bundleRequest, error) {
	cfg, _, err := a.loadConfig(false)
	if err != nil {
		return bundleRequest{}, err
	}
	baseDir := workflow.ConfigBaseDir(cfg)
	if len(cfg.Data.Bundles) == 0 {
		req.moduleDir = baseDir
		req.output = resolveBundlePath(baseDir, req.output)
		return req, nil
	}
	if !bundleFlagChanged(cmd) && !cmd.Flags().Changed("name") && len(cfg.Data.Bundles) > 1 {
		return bundleRequest{}, errors.New("multiple data bundles configured; pass --name or bundle flags")
	}
	var selected *workflow.DataBundleConfig
	if cmd.Flags().Changed("name") {
		for i := range cfg.Data.Bundles {
			if cfg.Data.Bundles[i].Name == req.name {
				selected = &cfg.Data.Bundles[i]
				break
			}
		}
		if selected == nil {
			return bundleRequest{}, fmt.Errorf("data bundle %q is not configured", req.name)
		}
	} else if len(cfg.Data.Bundles) == 1 {
		selected = &cfg.Data.Bundles[0]
	}
	if selected == nil {
		req.moduleDir = baseDir
		req.output = resolveBundlePath(baseDir, req.output)
		return req, nil
	}
	req = mergeBundleConfig(cmd, req, *selected)
	req = resolveConfigBundlePaths(cmd, req, baseDir)
	req.moduleDir = baseDir
	req.allowOutside = cfg.Paths.AllowOutsideProject
	return req, nil
}

func bundleFlagChanged(cmd *cobra.Command) bool {
	for _, name := range []string{
		"mode", "output", "package", "codec", "auto-threshold-bytes",
		"locales", "languages", "features", "scan-mode", "entrypoints", "scan",
		"feature-extra", "feature-exclude", "feature-rule",
	} {
		if flagChanged(cmd, name) {
			return true
		}
	}
	return false
}

func mergeBundleConfig(cmd *cobra.Command, req bundleRequest, cfg workflow.DataBundleConfig) bundleRequest {
	if !flagChanged(cmd, "name") {
		req.name = cfg.Name
	}
	if !flagChanged(cmd, "mode") {
		req.mode = cfg.Mode
	}
	if !flagChanged(cmd, "output") {
		req.output = cfg.Output
	}
	if !flagChanged(cmd, "package") {
		req.packageName = cfg.Package
	}
	if !flagChanged(cmd, "codec") {
		req.codec = cfg.Codec
	}
	if !flagChanged(cmd, "locales") {
		req.locales = cfg.Locales
	}
	if !flagChanged(cmd, "languages") {
		req.languages = cfg.Languages
	}
	if !flagChanged(cmd, "features") {
		req.features = cfg.Features
	}
	if !flagChanged(cmd, "scan-mode") {
		req.scanMode = cfg.ScanMode
	}
	if !flagChanged(cmd, "entrypoints") {
		req.entrypoints = cfg.Entrypoints
	}
	if !flagChanged(cmd, "scan") {
		req.scanRoots = cfg.ScanRoots
	}
	if !flagChanged(cmd, "feature-extra") {
		req.featureExtra = cfg.FeatureExtra
	}
	if !flagChanged(cmd, "feature-exclude") {
		req.featureExclude = cfg.FeatureExclude
	}
	if !flagChanged(cmd, "feature-rule") {
		req.featureRules = cfg.FeatureRules
	}
	if !flagChanged(cmd, "auto-threshold-bytes") {
		req.autoThreshold = cfg.AutoThresholdBytes
	}
	return req
}

func resolveConfigBundlePaths(cmd *cobra.Command, req bundleRequest, base string) bundleRequest {
	if base == "" {
		return req
	}
	resolve := func(path string) string {
		return resolveBundlePath(base, path)
	}
	req.output = resolve(req.output)
	if !flagChanged(cmd, "scan") {
		for i, root := range req.scanRoots {
			req.scanRoots[i] = resolve(root)
		}
	}
	return req
}

func flagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}

func planBundle(ctx context.Context, req bundleRequest) (bundleReport, error) {
	selection, scan, err := requestSelection(ctx, req)
	if err != nil {
		return bundleReport{}, err
	}
	_, plan, err := cldr.SelectBundle(cldr.BuiltinLean(), selection)
	if err != nil {
		return bundleReport{}, err
	}
	name := strings.TrimSpace(req.name)
	if name == "" {
		name = "custom"
	}
	return bundleReport{
		Version: 1, Name: name, Mode: normalizedMode(req.mode), Output: req.output, Package: req.packageName,
		Codec: normalizedCodecName(req.codec), Features: plan.Features, Locales: plan.Locales, Closure: plan.Closure,
		Scan: scan, AutoThreshold: req.autoThreshold,
	}, nil
}

func defaultBundleOutput(mode string) string {
	if mode == "embedded-pack" || mode == "native-go" {
		return "cldr_bundle.go"
	}
	return "cldr.cldrpack"
}

func resolveBundlePath(base, path string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if strings.TrimSpace(base) != "" {
		return filepath.Clean(filepath.Join(base, path))
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func generateBundle(ctx context.Context, req bundleRequest, dryRun bool) (bundleReport, error) {
	selection, scan, err := requestSelection(ctx, req)
	if err != nil {
		return bundleReport{}, err
	}
	selected, plan, err := cldr.SelectBundle(cldr.BuiltinLean(), selection)
	if err != nil {
		return bundleReport{}, err
	}
	codec, err := parseBundleCodec(req.codec)
	if err != nil {
		return bundleReport{}, err
	}
	pack, err := cldrpack.Build(selected, cldrpack.WithCodec(codec))
	if err != nil {
		return bundleReport{}, err
	}
	mode := normalizedMode(req.mode)
	if mode == "" {
		mode = "external-pack"
	}
	if mode == "auto" {
		if req.autoThreshold <= 0 {
			req.autoThreshold = 512 << 10
		}
		if int64(len(pack)) <= req.autoThreshold {
			mode = "embedded-pack"
		} else {
			mode = "external-pack"
		}
	}
	output := req.output
	if output == "" {
		output = defaultBundleOutput(mode)
	}
	output = resolveBundlePath(req.moduleDir, output)
	var data []byte
	switch mode {
	case "external-pack":
		data = pack
	case "embedded-pack":
		data, err = renderEmbeddedPack(req.packageName, pack)
	case "native-go":
		if !isBuiltinSelection(plan, cldr.BuiltinLean().Info()) {
			return bundleReport{}, fmt.Errorf("native-go mode currently supports only the full built-in lean selection; use embedded-pack or external-pack for custom selections")
		}
		data, err = renderNativeGo(req.packageName)
	default:
		return bundleReport{}, fmt.Errorf("unsupported bundle mode %q", mode)
	}
	if err != nil {
		return bundleReport{}, err
	}
	if !dryRun {
		if req.moduleDir != "" && !req.allowOutside {
			if err := requireInside(req.moduleDir, output); err != nil {
				return bundleReport{}, err
			}
		}
		if strings.HasSuffix(output, ".go") {
			// #nosec G304 -- bundle generation checks the caller-selected local output file.
			if existing, err := os.ReadFile(output); err == nil && !bytesContainsGeneratedHeader(existing) {
				return bundleReport{}, fmt.Errorf("refuse to overwrite non-generated Go file %s", output)
			}
		}
		if err := atomicfile.WriteFile(output, data, 0o644); err != nil {
			return bundleReport{}, err
		}
	}
	report := bundleReport{
		Version: 1, Name: req.name, Mode: mode, Output: output, Package: req.packageName,
		Codec: normalizedCodecName(req.codec), Features: plan.Features, Locales: plan.Locales, Closure: plan.Closure,
		Scan: scan, OutputBytes: len(data), AutoThreshold: req.autoThreshold, DryRun: dryRun,
		SizeReport: map[string]int{"pack_bytes": len(pack), "output_bytes": len(data)},
	}
	if report.Name == "" {
		report.Name = "custom"
	}
	return report, nil
}

func requireInside(base, path string) error {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return fmt.Errorf("path %s cannot be compared with project root %s: %w", path, base, err)
	}
	if rel == "." || rel == "" || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil
	}
	return fmt.Errorf("path %s escapes project root %s; set paths.allow_outside_project=true to allow it", path, base)
}

func requestSelection(ctx context.Context, req bundleRequest) (cldr.Selection, *extract.FeatureScanReport, error) {
	explicit, auto, err := expandFeatureSelectors(req.features)
	if err != nil {
		return cldr.Selection{}, nil, err
	}
	extra, extraAuto, err := expandFeatureSelectors(req.featureExtra)
	if err != nil {
		return cldr.Selection{}, nil, fmt.Errorf("feature-extra: %w", err)
	}
	if extraAuto {
		return cldr.Selection{}, nil, errors.New("feature-extra cannot include auto")
	}
	exclude, excludeAuto, err := expandFeatureSelectors(req.featureExclude)
	if err != nil {
		return cldr.Selection{}, nil, fmt.Errorf("feature-exclude: %w", err)
	}
	if excludeAuto {
		return cldr.Selection{}, nil, errors.New("feature-exclude cannot include auto")
	}
	features := append([]cldr.FeatureID{}, explicit...)
	features = append(features, extra...)
	var scan *extract.FeatureScanReport
	if auto {
		rules, err := parseFeatureRules(req.featureRules)
		if err != nil {
			return cldr.Selection{}, nil, err
		}
		report, err := extract.ScanCLDRFeatures(ctx, extract.FeatureScanOptions{
			Mode:           extract.FeatureScanMode(normalizedScanMode(req.scanMode)),
			ModuleDir:      req.moduleDir,
			Entrypoints:    req.entrypoints,
			ScanRoots:      req.scanRoots,
			OutputPaths:    []string{req.output},
			FeatureExtra:   extra,
			FeatureExclude: exclude,
			Rules:          rules,
		})
		if err != nil {
			return cldr.Selection{}, nil, err
		}
		scan = &report
		features = append(features, report.Features...)
	}
	features = removeExcludedFeatures(dedupeFeatures(features), exclude)
	return cldr.Selection{Locales: req.locales, Languages: req.languages, Features: features}, scan, nil
}

func expandFeatureSelectors(values []string) ([]cldr.FeatureID, bool, error) {
	var features []cldr.FeatureID
	auto := false
	for _, value := range splitSelectorValues(values) {
		if strings.EqualFold(value, "auto") {
			auto = true
			continue
		}
		expanded, err := expandRequestedFeatures([]string{value})
		if err != nil {
			return nil, false, err
		}
		features = append(features, expanded...)
	}
	return dedupeFeatures(features), auto, nil
}

func splitSelectorValues(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func parseFeatureRules(values []string) ([]extract.FeatureRule, error) {
	var rules []extract.FeatureRule
	for _, value := range values {
		for _, ruleText := range splitRuleValues(value) {
			target, rhs, ok := strings.Cut(ruleText, "=")
			if !ok {
				return nil, fmt.Errorf("feature rule %q must use target=feature[,feature]", ruleText)
			}
			target = strings.TrimSpace(target)
			if target == "" {
				return nil, fmt.Errorf("feature rule %q has empty target", ruleText)
			}
			features, auto, err := expandFeatureSelectors([]string{rhs})
			if err != nil {
				return nil, fmt.Errorf("feature rule %q: %w", ruleText, err)
			}
			if auto {
				return nil, fmt.Errorf("feature rule %q cannot include auto", ruleText)
			}
			if len(features) == 0 {
				return nil, fmt.Errorf("feature rule %q has no features", ruleText)
			}
			rules = append(rules, extract.FeatureRule{Target: target, Features: features})
		}
	}
	return rules, nil
}

func splitRuleValues(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func removeExcludedFeatures(features, exclude []cldr.FeatureID) []cldr.FeatureID {
	if len(exclude) == 0 {
		return features
	}
	excluded := map[cldr.FeatureID]bool{}
	for _, feature := range exclude {
		excluded[feature] = true
	}
	out := features[:0]
	for _, feature := range features {
		if !excluded[feature] {
			out = append(out, feature)
		}
	}
	return out
}

func dedupeFeatures(features []cldr.FeatureID) []cldr.FeatureID {
	seen := map[cldr.FeatureID]bool{}
	out := make([]cldr.FeatureID, 0, len(features))
	for _, feature := range features {
		if feature == "" || seen[feature] {
			continue
		}
		seen[feature] = true
		out = append(out, feature)
	}
	return out
}

func expandRequestedFeatures(values []string) ([]cldr.FeatureID, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := []cldr.FeatureID{}
	for _, value := range splitSelectorValues(values) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		switch value {
		case "all":
			out = append(out, cldr.FeatureID("all"))
		case "profile", "profile.defaults":
			out = append(out, cldr.FeatureProfileDefaults)
		case "numbers":
			out = append(out, cldr.FeatureNumbersSymbols, cldr.FeatureNumbersDecimal, cldr.FeatureNumbersPercent)
		case "currencies":
			out = append(out, cldr.FeatureCurrenciesFractions, cldr.FeatureCurrenciesSymbols)
		case "dates", "dates.gregorian":
			out = append(out, cldr.FeatureDatesGregorianPatterns, cldr.FeatureDatesGregorianNames)
		case "bcp47", "extensions":
			out = append(out, cldr.FeatureBCP47Extensions)
		default:
			out = append(out, cldr.FeatureID(value))
		}
	}
	return out, nil
}

func normalizedScanMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return string(extract.FeatureScanAuto)
	}
	return value
}

func parseBundleCodec(value string) (cldrpack.Codec, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "raw":
		return cldrpack.CodecRaw, nil
	case "zstd":
		return cldrpack.CodecZstd, nil
	default:
		return cldrpack.CodecRaw, fmt.Errorf("unsupported bundle codec %q", value)
	}
}

func normalizedCodecName(value string) string {
	if strings.TrimSpace(value) == "" {
		return "raw"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedMode(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func renderEmbeddedPack(packageName string, pack []byte) ([]byte, error) {
	if packageName == "" {
		packageName = "cldrbundle"
	}
	if err := validatePackageName(packageName); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("// Code generated by lingo; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "package %s\n\n", packageName)
	b.WriteString("import (\n")
	b.WriteString("\t\"github.com/nmeilick/go-i18n/locale/cldr\"\n")
	b.WriteString("\t\"github.com/nmeilick/go-i18n/locale/cldrpack\"\n")
	b.WriteString(")\n\n")
	b.WriteString("var packData = []byte{\n")
	for i, v := range pack {
		if i%12 == 0 {
			b.WriteString("\t")
		}
		fmt.Fprintf(&b, "0x%02x, ", v)
		if i%12 == 11 {
			b.WriteString("\n")
		}
	}
	if len(pack)%12 != 0 {
		b.WriteString("\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("func Bundle() cldr.Bundle {\n")
	b.WriteString("\tbundle, err := cldrpack.FromEmbedded(\"embedded.cldrpack\", packData)\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	b.WriteString("\treturn bundle\n")
	b.WriteString("}\n")
	return goformat.Source([]byte(b.String()))
}

func renderNativeGo(packageName string) ([]byte, error) {
	if packageName == "" {
		packageName = "cldrbundle"
	}
	if err := validatePackageName(packageName); err != nil {
		return nil, err
	}
	src := "// Code generated by lingo; DO NOT EDIT.\n" +
		"package " + packageName + "\n\n" +
		"import \"github.com/nmeilick/go-i18n/locale/cldr\"\n\n" +
		"func Bundle() cldr.Bundle { return cldr.BuiltinLean() }\n"
	return goformat.Source([]byte(src))
}

func validatePackageName(name string) error {
	if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
		return fmt.Errorf("invalid Go package name %q", name)
	}
	return nil
}

func bytesContainsGeneratedHeader(data []byte) bool {
	return strings.Contains(string(data[:min(len(data), 256)]), "Code generated")
}

func isBuiltinSelection(plan cldr.SelectionPlan, info cldr.Info) bool {
	return len(plan.Features) == len(info.Features) && len(plan.Locales) == len(info.Locales)
}

func reportFromBundle(bundle cldr.Bundle, output string) bundleReport {
	info := bundle.Info()
	report := bundleReport{
		Version: 1, Name: info.Name, Mode: info.Mode, Output: output,
		Features: info.Features, Locales: info.Locales,
	}
	if report.Name == "" {
		report.Name = info.ID
	}
	return report
}

func (a *app) writeBundleReport(cmd *cobra.Command, report bundleReport) error {
	if a.jsonOut {
		return a.writeJSON(cmd, report)
	}
	out := cmd.OutOrStdout()
	writef := func(format string, args ...any) error {
		_, err := fmt.Fprintf(out, format, args...)
		return err
	}
	write := func(s string) error {
		_, err := fmt.Fprint(out, s)
		return err
	}
	if report.Output != "" {
		if err := writef("bundle %s mode=%s output=%s", report.Name, report.Mode, report.Output); err != nil {
			return err
		}
	} else {
		if err := writef("bundle %s mode=%s", report.Name, report.Mode); err != nil {
			return err
		}
	}
	if report.Codec != "" {
		if err := writef(" codec=%s", report.Codec); err != nil {
			return err
		}
	}
	if report.OutputBytes > 0 {
		if err := writef(" bytes=%d", report.OutputBytes); err != nil {
			return err
		}
	}
	if report.DryRun {
		if err := write(" dry-run=true"); err != nil {
			return err
		}
	}
	if report.HashVerified {
		if err := write(" hash=ok"); err != nil {
			return err
		}
	}
	if report.Scan != nil {
		if err := writef(" scan_features=%d", len(report.Scan.Features)); err != nil {
			return err
		}
		if len(report.Scan.Warnings) > 0 {
			if err := writef(" scan_warnings=%d", len(report.Scan.Warnings)); err != nil {
				return err
			}
		}
	}
	if err := writef(" features=%d locales=%d\n", len(report.Features), len(report.Locales)); err != nil {
		return err
	}
	if a.verbose > 0 {
		if err := writef("features: %s\n", joinFeatureIDs(report.Features)); err != nil {
			return err
		}
		if err := writef("locales: %s\n", strings.Join(report.Locales, ",")); err != nil {
			return err
		}
		if len(report.Closure) > 0 {
			if err := writef("closure: %s\n", strings.Join(report.Closure, ",")); err != nil {
				return err
			}
		}
		if report.Scan != nil {
			if err := writef("scan features: %s\n", joinFeatureIDs(report.Scan.Features)); err != nil {
				return err
			}
			for _, pkg := range report.Scan.Packages {
				if err := writef("scan package: %s reason=%s\n", pkg.ImportPath, pkg.Reason); err != nil {
					return err
				}
			}
			for _, usage := range report.Scan.Usages {
				if err := writef("scan usage: %s %s:%d %s reason=%s\n",
					usage.Feature, usage.Path, usage.Line, usage.Symbol, usage.Reason); err != nil {
					return err
				}
			}
			for _, warning := range report.Scan.Warnings {
				if err := writef("scan warning: %s %s:%d %s\n",
					warning.Code, warning.Path, warning.Line, warning.Message); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func joinFeatureIDs(features []cldr.FeatureID) string {
	values := make([]string, len(features))
	for i, feature := range features {
		values[i] = string(feature)
	}
	return strings.Join(values, ",")
}

func (a *app) loadConfig(requireFile bool) (workflow.Config, string, error) {
	loc := selectConfigLocation(a.configLocations())
	if loc.Exists {
		cfg, err := workflow.LoadConfig(loc.Path)
		return cfg, loc.Path, err
	}
	if loc.Required || requireFile {
		return workflow.Config{}, loc.Path, fmt.Errorf("config file %s does not exist; run lingo config init", loc.Path)
	}
	cfg := workflow.DefaultConfig()
	cfg = workflow.WithBaseDir(cfg, mustAbs("."))
	return cfg, loc.Path, nil
}

func (a *app) configLocations() []configLocation {
	if strings.TrimSpace(a.configPath) != "" {
		return []configLocation{configLocationFor("flag", a.configPath, true)}
	}
	if env := strings.TrimSpace(os.Getenv("LINGO_CONFIG")); env != "" {
		return []configLocation{configLocationFor("env", env, true)}
	}
	locations := []configLocation{}
	if project := findProjectConfig(); project != "" {
		locations = append(locations, configLocationFor("project", project, false))
	} else {
		locations = append(locations, configLocationFor("project", defaultProjectConfigPath(), false))
	}
	if xdg := xdgConfigPath(); xdg != "" {
		locations = append(locations, configLocationFor("xdg", xdg, false))
	}
	return locations
}

func selectConfigLocation(locations []configLocation) configLocation {
	if len(locations) == 0 {
		return configLocationFor("project", defaultProjectConfigPath(), false)
	}
	for _, loc := range locations {
		if loc.Exists || loc.Required {
			return loc
		}
	}
	return locations[0]
}

func configLocationFor(source, path string, required bool) configLocation {
	path = mustAbs(path)
	_, err := os.Stat(path)
	return configLocation{Path: path, Source: source, Exists: err == nil, Required: required}
}

func findProjectConfig() string {
	dir := mustAbs(".")
	for {
		path := filepath.Join(dir, "lingo.toml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}

func defaultProjectConfigPath() string {
	return filepath.Join(mustAbs("."), "lingo.toml")
}

func xdgConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, AppName, "config.toml")
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func resolveEditor(flag string) string {
	for _, value := range []string{flag, os.Getenv("LINGO_EDITOR"), os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for _, candidate := range []string{"/usr/bin/sensible-editor", "editor", "nano", "vim", "vi"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

func (a *app) write(cmd *cobra.Command, obj any, format string, args ...any) error {
	if a.jsonOut {
		return a.writeJSON(cmd, obj)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), format, args...)
	return err
}

func (a *app) writeJSON(cmd *cobra.Command, obj any) error {
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}
