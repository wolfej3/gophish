// Command assets builds the admin interface's JavaScript and CSS bundles in
// static/js/dist and static/css/dist using esbuild. Run it from the
// repository root after changing anything in static/js/src or static/css:
//
//	go run ./tools/assets
//
// The bundles are committed, and CI checks that they match their sources.
package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

const (
	vendorDir  = "static/js/src/vendor"
	appDir     = "static/js/src/app"
	cssDir     = "static/css"
	jsDistDir  = "static/js/dist"
	cssDistDir = "static/css/dist"
)

// target is the JavaScript version the bundles are compiled down to.
const target = api.ES2017

// vendorScripts are concatenated, in order, into vendor.min.js. They're
// plain scripts that define globals the app scripts rely on, so the order
// matters.
var vendorScripts = []string{
	"jquery.js",
	"bootstrap.min.js",
	"moment.min.js",
	"papaparse.min.js",
	"d3.min.js",
	"topojson.min.js",
	"datamaps.min.js",
	"jquery.dataTables.min.js",
	"dataTables.bootstrap.js",
	"datetime-moment.js",
	"jquery.ui.widget.js",
	"jquery.fileupload.js",
	"jquery.iframe-transport.js",
	"sweetalert2.min.js",
	"bootstrap-datetime.js",
	"select2.min.js",
	"core.min.js",
	"highcharts.js",
	"ua-parser.min.js",
}

// appScripts are minified individually into app/<name>.min.js. They're
// plain scripts whose top-level functions are called from the templates
// (for example, in onclick handlers), so they're not wrapped or bundled.
var appScripts = []string{
	"audit_log.js",
	"autocomplete.js",
	"campaign_results.js",
	"campaigns.js",
	"dashboard.js",
	"groups.js",
	"landing_pages.js",
	"people.js",
	"sending_profiles.js",
	"settings.js",
	"teams.js",
	"templates.js",
	"gophish.js",
	"users.js",
	"webhooks.js",
}

// moduleScripts are ES modules bundled with their imports into
// app/<name>.min.js.
var moduleScripts = []string{
	"passwords.js",
}

// vendorModules maps the packages imported by moduleScripts to their
// vendored copies.
var vendorModules = map[string]string{
	"zxcvbn": filepath.Join(vendorDir, "zxcvbn", "zxcvbn.js"),
}

// charsetRule matches a CSS @charset rule.
var charsetRule = regexp.MustCompile(`@charset\s+"[^"]*";`)

// styles are concatenated, in order, into gophish.css.
var styles = []string{
	"bootstrap.min.css",
	"main.css",
	"dashboard.css",
	"flat-ui.css",
	"dataTables.bootstrap.css",
	"font-awesome.min.css",
	"chartist.min.css",
	"bootstrap-datetime.css",
	"checkbox.css",
	"sweetalert2.min.css",
	"select2.min.css",
	"select2-bootstrap.min.css",
}

func main() {
	log.SetFlags(0)
	if err := build(); err != nil {
		log.Fatal(err)
	}
}

func build() error {
	vendor, err := concat(vendorDir, vendorScripts, "\n;\n")
	if err != nil {
		return err
	}
	if err := transform(vendor, "vendor.js", api.LoaderJS, filepath.Join(jsDistDir, "vendor.min.js")); err != nil {
		return err
	}
	for _, name := range appScripts {
		src, err := os.ReadFile(filepath.Join(appDir, name))
		if err != nil {
			return err
		}
		if err := transform(src, name, api.LoaderJS, appOutput(name)); err != nil {
			return err
		}
	}
	for _, name := range moduleScripts {
		if err := bundle(filepath.Join(appDir, name), appOutput(name)); err != nil {
			return err
		}
	}
	css, err := concat(cssDir, styles, "\n")
	if err != nil {
		return err
	}
	// @charset is only valid at the very start of a stylesheet, so the ones
	// in the individual files are dropped. Everything is UTF-8, which is also
	// the charset the CSS is served with.
	css = charsetRule.ReplaceAll(css, nil)
	return transform(css, "gophish.css", api.LoaderCSS, filepath.Join(cssDistDir, "gophish.css"))
}

// appOutput returns the bundle path for the app script with the given name.
func appOutput(name string) string {
	return filepath.Join(jsDistDir, "app", strings.TrimSuffix(name, ".js")+".min.js")
}

// concat joins the named files in dir, in order, with sep between them.
func concat(dir string, names []string, sep string) ([]byte, error) {
	var buf bytes.Buffer
	for i, name := range names {
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if i > 0 {
			buf.WriteString(sep)
		}
		buf.Write(src)
	}
	return buf.Bytes(), nil
}

// transform minifies src without bundling it and writes the result to out.
// Top-level names are kept, since other scripts and the templates use them.
func transform(src []byte, name string, loader api.Loader, out string) error {
	result := api.Transform(string(src), api.TransformOptions{
		Loader:            loader,
		Sourcefile:        name,
		Target:            target,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		LogLevel:          api.LogLevelWarning,
	})
	if len(result.Errors) > 0 {
		return fmt.Errorf("building %s: %s", name, result.Errors[0].Text)
	}
	return write(out, result.Code)
}

// bundle bundles the ES module at entry with its imports into a single
// script and writes it to out.
func bundle(entry, out string) error {
	result := api.Build(api.BuildOptions{
		EntryPoints:       []string{entry},
		Bundle:            true,
		Format:            api.FormatIIFE,
		Target:            target,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		LogLevel:          api.LogLevelWarning,
		Plugins:           []api.Plugin{vendorPlugin()},
	})
	if len(result.Errors) > 0 {
		return fmt.Errorf("building %s: %s", entry, result.Errors[0].Text)
	}
	return write(out, result.OutputFiles[0].Contents)
}

// vendorPlugin resolves imports of the packages in vendorModules to their
// vendored files, so no package manager is needed.
func vendorPlugin() api.Plugin {
	return api.Plugin{
		Name: "vendor",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: ".*"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				path, ok := vendorModules[args.Path]
				if !ok {
					return api.OnResolveResult{}, nil
				}
				abs, err := filepath.Abs(path)
				return api.OnResolveResult{Path: abs}, err
			})
		},
	}
}

func write(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, contents, 0644)
}
