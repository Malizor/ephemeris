// This is the driver for our blog-compiler, which will generate a static-blog
// from a series of text-files.
//
// We use the `ephemeris` package to find the text-files beneath a given
// root-directory, and then interate over them in various ways to build up:
//
// * The blog-entries themselves
//
// * The tag-cloud
//
// * An archive.
//
// * The index & RSS feed.
//
// The way that the system is setup the most recent post will allow
// comments to be submitted upon it - all others will be read-only.
//
package main

import (
	"embed"
	"flag"
	"fmt"
	"html"
	"io/fs"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/skx/ephemeris"
)

//
// Embedded template-resources
//

// TEMPLATES holds our embedded template resources.
//go:embed data/**
var TEMPLATES embed.FS

//
// Variables which are reused live here.
//

// We generate a static-version (read:embedded copy) of all the
// template-files beneath `data/`.
//
// We load each of these as golang templates here, so that they
// are globally available and we don't have to re-read/re-parse them
// more than once.
//
// These are stored as part of the build-process, in the `TEMPLATES`
// variable above.
var tmpl *template.Template

// We load a JSON configuration file when we launch, which contains
// the mandatory settings.  We make this configuration object global
// to access those variables even though that is a bad design.
var config Config

// mkdirIfMissing makes a directory, if it is missing.
//
// The overhead of calling `stat` probably makes it cheaper to just
// always call `mkdir` and ignore the error, but this is cleaner.
func mkdirIfMissing(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(path, 0755)
	}
}

// loadTemplates returns a collection of all the templates we have
// embedded within our application.
//
// In addition to loading the templates we also populate a function-map,
// to allow various functions to be made available to all templates.
//
// The functions defined are:
//
// ISO8601          - Needed for RSS generation.
// LOWER            - Lower-case a string.  Used for link-generation.
// ESCAPE           - Escape HTML-text for RSS_generation too.
// RECENT_POST_DATE - The date format used for the "most recent entries" list in the sidebar.
// BLOG_POST_DATE   - The format used in the index/archive/tag-view.
//
func loadTemplates() (*template.Template, error) {

	// Create a helper-template, with no name.
	t := template.New("").Funcs(template.FuncMap{

		// Date-format for RSS feed
		"ISO8601": func(d time.Time) string {
			return (fmt.Sprintf("%v", d.Format(time.RFC3339)))
		},

		// Escape HTML in RSS feed
		"ESCAPE": func(in string) string {
			return (html.EscapeString(in))
		},

		// Escape link name.
		"ESCAPE_LINK": func(in string) string {
			return (url.PathEscape(in))
		},

		// Convert a string to lower-case.
		//
		// This is used to make sure all links point to their
		// lower-cased version of the URL.
		//
		"LOWER": func(in string) string {
			return (strings.ToLower(in))
		},

		// Prefix of the blog - i.e. URL to prepend to links
		"PREFIX": func() string {
			return config.Prefix
		},

		// Date used on "recent posts"
		"RECENT_POST_DATE": func(d time.Time) string {
			year, month, day := d.Date()
			return (fmt.Sprintf("%d %s %d", day, month.String(), year))
		},

		// Date used on all blog posts.
		"BLOG_POST_DATE": func(d time.Time) string {
			year, month, day := d.Date()
			return (fmt.Sprintf("%d %s %d %02d:%02d", day, month.String(), year, d.Hour(), d.Minute()))
		},

		// Date used on comments.
		"COMMENT_POST_DATE": func(d time.Time) string {
			year, month, day := d.Date()
			return (fmt.Sprintf("at %02d:%02d on %d %s %d", d.Hour(), d.Minute(), day, month.String(), year))
		},
	})

	//
	// We're either going to walk a theme-directory, which is
	// a local directory hierarchy, or we're going to walk
	// the embedded resources.
	//
	//
	// Default to the embedded resources
	var arg fs.FS
	arg = TEMPLATES

	// But if we have a path then use that.
	if config.ThemePath != "" {
		arg = os.DirFS(config.ThemePath)
	}

	// Now load all the templates
	err := fs.WalkDir(arg, ".", func(pth string, d fs.DirEntry, err error) error {
		// Error?  Then return it
		if err != nil {
			return err
		}

		// Directory?  Ignore it.
		if d.IsDir() {
			return nil
		}

		//
		// Contents of the template we're loading.
		//
		var data []byte

		// Get the contents of the file.
		//
		// Either from the local-path
		if config.ThemePath != "" {

			// Complete path
			complete := path.Join(config.ThemePath, pth)

			// Read the file contents
			data, err = ioutil.ReadFile(complete)
			if err != nil {
				return err
			}
		} else {

			// Or from the embedded resource.
			data, err = TEMPLATES.ReadFile(pth)
			if err != nil {
				return err
			}

			// However if we're loading from the
			// embedded resource we need to strip
			// the "data/" prefix.
			pth = strings.TrimPrefix(pth, "data/")
		}

		// Add the data + template
		t = t.New(pth)
		t, err = t.Parse(string(data))
		if err != nil {
			return err
		}

		return nil
	})

	return t, err
}

// exportDefaultTheme iterates over each of our template-resources and writes
// them to the specified path.
func exportDefaultTheme(prefix string) {

	// Now load all the templates
	err := fs.WalkDir(TEMPLATES, ".", func(pth string, d fs.DirEntry, err error) error {
		// Error?  Then return it
		if err != nil {
			return err
		}

		// Directory?  Make sure it exists.
		if d.IsDir() {
			if d.Name() != "data" {
				tmp := path.Join(prefix, d.Name())
				fmt.Printf("Creating directory %s\n", tmp)
				mkdirIfMissing(tmp)
			}
			return nil
		}

		// Where we write the data to
		fileOut := path.Join(prefix, strings.TrimPrefix(pth, "data/"))

		// Copy file contents..
		fmt.Printf("Copying %s to %s\n", pth, fileOut)

		// Get the file contents.
		data, err := TEMPLATES.ReadFile(pth)
		if err != nil {
			return err
		}

		// Write the contents
		err = ioutil.WriteFile(fileOut, data, 0644)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error exporting theme:%s\n", err.Error())
	}
}

// outputTags writes out the tag-specific pages.
//
// First of all build up a list of tags, then render
// a template for each one.
func outputTags(posts []ephemeris.BlogEntry, recentPosts []ephemeris.BlogEntry) error {

	//
	// OK we'll now try to build up a list of tags.
	//
	// The tag list will consist of:
	//
	//   tagName -> [array-index-1, array-index-2, ...]
	//
	// Where the array indexes are the indexes of the posts
	// array which contain the tag.
	//
	// We do this because it is lightweight.
	//
	tagMap := make(map[string][]int)

	//
	// For each post ..
	//
	for i, e := range posts {

		// If it has tags ..
		if len(e.Tags) > 0 {

			// For each tag
			for _, tag := range e.Tags {

				// Add the entry-number to the tag-list
				existing := tagMap[tag]
				existing = append(existing, i)
				tagMap[tag] = existing
			}
		}
	}

	//
	//  Page-Structure for a tag-page view.
	//
	//  i.e. /tags/z80
	//
	type TagPage struct {

		// Tag contains the name of the tag.
		Tag string

		// Entries holds entries having the given tag
		Entries []ephemeris.BlogEntry

		// RecentPosts contains data for our sidebar.
		RecentPosts []ephemeris.BlogEntry
	}

	//
	// Create an instance.
	//
	var pageData TagPage
	pageData.RecentPosts = recentPosts

	//
	// Create a per-page tag-template
	//
	for key, uses := range tagMap {

		mkdirIfMissing(filepath.Join(config.OutputPath, "tags", key))

		// Empty the tags from the previous run
		pageData.Entries = nil
		pageData.Tag = key

		// Add the entries
		for _, e := range uses {
			pageData.Entries = append(pageData.Entries, posts[e])
		}

		// Sort by date - tags will be viewed in creation-order
		sort.Slice(pageData.Entries, func(i, j int) bool {
			a := pageData.Entries[i].Date
			b := pageData.Entries[j].Date
			return a.Before(b)
		})

		//
		// Create the output file.
		//
		output, err := os.Create(filepath.Join(config.OutputPath, "tags", key, "index.html"))
		if err != nil {
			return err
		}

		//
		// Render the template into our file.
		//
		err = tmpl.ExecuteTemplate(output, "tag_page.tmpl", pageData)
		if err != nil {
			return err
		}
		output.Close()
	}

	//
	// /tags/index.html
	//

	//
	// We want a sorted list of tags
	//
	var tagNames []string
	for key := range tagMap {
		tagNames = append(tagNames, key)
	}
	sort.Strings(tagNames)

	//
	// We want to have a tag-cloud for the /tags/index.html page.
	//
	type TagMap struct {
		// Tag contains the name of the string.
		Tag string

		// TSize contains the text-size of the entry in the cloud.
		TSize int

		// Count shows how many times the tag was used.
		Count int
	}

	//
	// Page-data: The tag-data, and the recent-posts list.
	//
	type TagCloudPage struct {
		Tags        []TagMap
		RecentPosts []ephemeris.BlogEntry
	}
	var tagCloud TagCloudPage
	tagCloud.RecentPosts = recentPosts

	//
	// Now we have a sorted list of unique tag-names we can build up
	// that array for the template-page
	//
	for _, tag := range tagNames {
		count := len(tagMap[tag])
		size := (count * 5) + 5
		if size > 60 {
			size = 60
		}
		tagCloud.Tags = append(tagCloud.Tags, TagMap{Tag: tag, Count: count, TSize: size})
	}

	//
	// Create the output file
	//
	ti, err := os.Create(filepath.Join(config.OutputPath, "tags", "index.html"))
	if err != nil {
		return err
	}

	//
	// Render the template into our file.
	//
	err = tmpl.ExecuteTemplate(ti, "tags.tmpl", tagCloud)
	if err != nil {
		return err
	}
	ti.Close()

	return nil
}

// output a year/month page for each distinct period in which we have posts.
func outputArchive(posts []ephemeris.BlogEntry, recentPosts []ephemeris.BlogEntry) error {

	//
	// We'll build up a list of year/mon pages.
	//
	// The map will consist of:
	//
	//   year/mon -> [array-index-1, array-index-2, ...]
	//
	// Where the array indexes are the indexes of the posts
	// array which were posted in the given month.
	//
	archiveMap := make(map[string][]int)

	for i, e := range posts {

		// The key is "YYYY/NN"
		key := e.Year() + "/" + e.MonthNumber()

		existing := archiveMap[key]
		existing = append(existing, i)
		archiveMap[key] = existing
	}

	//
	// Archive page contains data to be shown in
	// the archive page of:
	//
	//  /archive/2019/03/
	//
	type PageData struct {

		// Year contains the year we're covering.
		Year string

		// Month contains the month we're covering.
		Month string

		// Entries holds the entries in the given year/month
		Entries []ephemeris.BlogEntry

		// RecentPosts holds data for the sidebar.
		RecentPosts []ephemeris.BlogEntry
	}

	//
	// Create an instance of the object.
	//
	var pageData PageData
	pageData.RecentPosts = recentPosts

	//
	// Create a per-page output
	//
	for key, uses := range archiveMap {

		mkdirIfMissing(filepath.Join(config.OutputPath, "archive", key))

		// Empty the tags from the previous run
		pageData.Entries = nil

		// Add the entries
		for _, e := range uses {
			pageData.Entries = append(pageData.Entries, posts[e])

			//
			// Our archiveMap contains keys of the form:
			//
			//    year/mon
			//
			// But when we present this to the viewers we want
			// to show the month-name, and year name.
			//
			// We can calculate that from the `Date` field
			// in the post itself :)
			//
			pageData.Year = posts[e].Year()
			pageData.Month = posts[e].MonthName()
		}

		// Sort by date - posts will be in order they've been written
		sort.Slice(pageData.Entries, func(i, j int) bool {
			a := pageData.Entries[i].Date
			b := pageData.Entries[j].Date
			return a.Before(b)
		})

		//
		// Create the output file.
		//
		output, err := os.Create(filepath.Join(config.OutputPath, "archive", key, "index.html"))
		if err != nil {
			return err
		}

		//
		// Render the template into it.
		//
		err = tmpl.ExecuteTemplate(output, "archive_page.tmpl", pageData)
		if err != nil {
			return err
		}
		output.Close()
	}

	//
	// Page data for the archive-index.
	//
	//  i.e. /archive/index.html
	//
	type ArchiveCount struct {
		Year      string
		Month     string
		MonthName string
		Count     string
	}

	mappy := make(map[string][]ArchiveCount)

	type ArchiveIndex struct {
		Year        string
		Data        []ArchiveCount
		RecentPosts []ephemeris.BlogEntry
	}
	var ai []ArchiveIndex

	//
	// Build up the count of posts in the given month/year.
	//
	for _, e := range archiveMap {

		//
		// Since all the posts are have the same year + month
		// pair we're able to just use the first entry in
		// each returned set.
		//
		y := posts[e[0]].Year()
		m := posts[e[0]].MonthNumber()
		n := posts[e[0]].MonthName()
		c := fmt.Sprintf("%d", len(e))

		// Append
		existing := mappy[y]
		existing = append(existing, ArchiveCount{
			Count:     c,
			Year:      y,
			Month:     m,
			MonthName: n,
		})

		mappy[y] = existing
	}

	// Sort the entries now we've generated them.
	for k, entries := range mappy {

		vals := mappy[k]
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Month < entries[j].Month
		})
		mappy[k] = vals

	}

	//
	// Now a data-structure
	//
	// sorted keys
	//
	var years []string
	for year := range mappy {
		years = append(years, year)
	}
	sort.Strings(years)

	//
	// For each year we add the data
	//
	for _, year := range years {
		ai = append(ai, ArchiveIndex{Year: year, Data: mappy[year], RecentPosts: recentPosts})
	}

	//
	// Create the output file.
	//
	ar, err := os.Create(filepath.Join(config.OutputPath, "archive", "index.html"))
	if err != nil {
		return err
	}

	//
	// Render the template into our file.
	//
	err = tmpl.ExecuteTemplate(ar, "archive.tmpl", ai)
	if err != nil {
		return err
	}
	ar.Close()
	return nil
}

// outputIndex outputs the /index.html file.
//
// We don't need to sort, or limit ourselves here, because we only use
// the "most recent posts" we've already discovered.
//
func outputIndex(posts []ephemeris.BlogEntry, recentPosts []ephemeris.BlogEntry) error {

	mkdirIfMissing(config.OutputPath)

	// Page-structure for the site.
	type Recent struct {

		// Entries has the most recent entries.
		Entries []ephemeris.BlogEntry

		// RecentPosts has the same data, but for
		// the side-bar.  It is redundant.
		RecentPosts []ephemeris.BlogEntry
	}

	//
	// The data we'll store for the page.
	//
	// Our front-page shows the same number of posts as
	// the recent-list in the sidebar, so we don't need
	// to do anything special here, we show the same
	// list for both of them.
	//
	var pageData Recent
	pageData.Entries = recentPosts
	pageData.RecentPosts = recentPosts

	//
	// Create the output file.
	//
	output, err := os.Create(filepath.Join(config.OutputPath, "index.html"))
	if err != nil {
		return err
	}

	//
	// Render the template into our file.
	//
	err = tmpl.ExecuteTemplate(output, "index.tmpl", pageData)
	if err != nil {
		return err
	}
	output.Close()

	return nil

}

// outputRSS outputs the /index.rss file.
//
// We don't need to sort, or limit ourselves here, because we only use
// the "most recent posts" we've already discovered.
//
func outputRSS(posts []ephemeris.BlogEntry, recentPosts []ephemeris.BlogEntry) error {

	mkdirIfMissing(config.OutputPath)

	// Page-structure for the site.
	type Recent struct {

		// Entries has the most recent entries.
		Entries []ephemeris.BlogEntry

		// RecentPosts has the same data, but for
		// the side-bar.  It is redundant.
		RecentPosts []ephemeris.BlogEntry
	}

	//
	// The data we'll store for the page.
	//
	// Our front-page shows the same number of posts as
	// the recent-list in the sidebar, so we don't need
	// to do anything special here, we show the same
	// list for both of them.
	//
	var pageData Recent
	pageData.Entries = recentPosts
	pageData.RecentPosts = recentPosts

	//
	// Create the output file.
	//
	rss, err := os.Create(filepath.Join(config.OutputPath, "index.rss"))
	if err != nil {
		return err
	}

	//
	// Render the template into it.
	//
	err = tmpl.ExecuteTemplate(rss, "index.rss", pageData)
	if err != nil {
		return err
	}
	rss.Close()

	return nil

}

// Output one page for each entry.
//
// If comments are enabled then we'll add the comments to the entries,
// and we'll ensure we setup the comment CGI path.
func outputEntries(posts []ephemeris.BlogEntry, recentPosts []ephemeris.BlogEntry) error {

	mkdirIfMissing(config.OutputPath)

	// Page-structure for the site.
	type Recent struct {

		// The blog-entry
		Entry ephemeris.BlogEntry

		// Should we display the add-comment form for this post?
		AddComment bool

		// CGI link
		CommentAPI string

		// The recent posts for the sidebar.
		RecentPosts []ephemeris.BlogEntry
	}

	//
	// The data we use for output.
	//
	var pageData Recent

	// The most recent posts
	pageData.RecentPosts = recentPosts
	pageData.AddComment = false

	// The site prefix, and the link to the CGI form for
	// comment-submission.
	pageData.CommentAPI = config.CommentAPI

	//
	// Create a per-page output
	//
	for _, entry := range posts {

		//
		// Populate the page-data with this entry.
		//
		pageData.Entry = entry

		//
		// The most recent post has comments enabled,
		// all others do not.
		//
		pageData.AddComment = config.AddComments && (entry.Path == recentPosts[0].Path)

		//
		// We have a link and that points to a filename.
		//
		// We get the latter from the former by removing the
		// prefix.
		//
		u, err := url.Parse(entry.Link)
		if err != nil {
			return err
		}

		// Get the path.
		path := u.RequestURI()

		// Remove the leading slash.
		path = strings.TrimPrefix(path, "/")

		//
		// Lower-case PATH and write to that too
		//
		dest := strings.ToLower(path)

		//
		// Create the output file.
		//
		output, err := os.Create(filepath.Join(config.OutputPath, dest))
		if err != nil {
			return err
		}

		//
		// Render the template into it.
		//
		err = tmpl.ExecuteTemplate(output, "entry.tmpl", pageData)
		if err != nil {
			return err
		}
		output.Close()

		//
		// Create symlink
		//
		os.Symlink(dest, filepath.Join(config.OutputPath, path))

	}

	return nil

}

// main is our entry-point.
func main() {

	//
	// Command-line arguments which are accepted.
	//
	allowComments := flag.Bool("allow-comments", true, "Enable comments to be added to the most recent entry.")
	confFile := flag.String("config", "ephemeris.json", "The path to our configuration file.")
	exportTheme := flag.String("export-theme", "", "Export the default theme to a local directory.")

	//
	// Parse the flags.
	//
	flag.Parse()

	//
	// Exporting the theme?
	//
	if *exportTheme != "" {
		exportDefaultTheme(*exportTheme)
		return
	}

	//
	// Record our start-time
	//
	start := time.Now()

	//
	// Load our configuration file (JSON)
	//
	var err error
	config, err = loadConfig(*confFile)
	if err != nil {
		fmt.Printf("Failed to load configuration file %s %s\n", *confFile, err.Error())
		return
	}

	//
	// Setup defaults if missing
	//
	if config.OutputPath == "" {
		config.OutputPath = "output"
	}
	if config.PostsPath == "" {

		// Migration of legacy key-name
		if config.Posts != "" {
			config.PostsPath = config.Posts
		} else {
			config.PostsPath = "data/"
		}
	}
	if config.CommentsPath == "" {
		// Migration of legacy key-name
		if config.Comments != "" {
			config.CommentsPath = config.Comments
		} else {
			config.CommentsPath = "comments/"
		}
	}

	//
	// Preserve comment setting, and theme-path
	//
	config.AddComments = *allowComments

	//
	// Create an object to generate our blog from
	//
	site, err := ephemeris.New(config.PostsPath, config.CommentsPath, config.Prefix)
	if err != nil {
		fmt.Printf("Failed to create site: %s\n", err.Error())
		return
	}

	//
	// Get all the entries, and the recent entries too.
	//
	entries := site.Entries()
	recent := site.Recent(10)

	//
	// Show the number of blog-posts we processed.
	//
	fmt.Printf("Read %d blog posts.\n", len(entries))

	//
	// We can now load the collection of templates which we've stored
	// in `static.go`.
	//
	// Our templates are loaded en masse, and each one of them
	// has some (custom/bonus/extra) functions available to them.
	//
	tmpl, err = loadTemplates()
	if err != nil {
		fmt.Printf("Error loading embedded resources: %s\n", err.Error())
		return
	}

	//
	// We're going to run the page-generation in a series of threads
	// now.  So we'll add a synchronizer here.
	//
	var wg sync.WaitGroup

	//
	// Ensure we use all the CPU we have available.
	//
	runtime.GOMAXPROCS(runtime.NumCPU())

	// We're going to wait for all our routines to be complete,
	// fixed number here, as added below:
	wg.Add(5)

	//
	// Output tag-cloud, and per-tag pages.
	//
	go func() {
		err := outputTags(entries, recent)
		if err != nil {
			fmt.Printf("Error rendering tag-pages:%s\n", err.Error())
			os.Exit(1)
		}
		wg.Done()
	}()

	//
	// Output the per year/month archive, and the archive-index.
	//
	go func() {
		err := outputArchive(entries, recent)
		if err != nil {
			fmt.Printf("Error rendering archive-pages:%s\n", err.Error())
			os.Exit(1)
		}
		wg.Done()
	}()

	//
	// Output index page.
	//
	go func() {
		err := outputIndex(entries, recent)
		if err != nil {
			fmt.Printf("Error rendering index.html: %s\n", err.Error())
			os.Exit(1)
		}
		wg.Done()
	}()

	//
	// Output RSS feed which has the same information as the index-page.
	//
	go func() {
		err := outputRSS(entries, recent)
		if err != nil {
			fmt.Printf("Error rendering /index.rss: %s\n", err.Error())
			os.Exit(1)
		}
		wg.Done()
	}()

	//
	// Output each entry.
	//
	go func() {
		err := outputEntries(entries, recent)

		if err != nil {
			fmt.Printf("Error rendering blog-posts: %s\n", err.Error())
			os.Exit(1)
		}
		wg.Done()
	}()

	wg.Wait()

	//
	// Report on our runtime
	//
	elapsed := time.Since(start)
	fmt.Printf("Compilation took %s\n", elapsed)

}
