
# Comment Support

`ephemeris` supports the submission of comments upon published posts, via an optional CGI script.

This document describes how you would go about enabling this support.



# Comment Overview

The basic use of `ephemeris` is to convert a collection of text files into a HTML & RSS blog.

The input to the compilation process is:

* A set of plain-text blog-posts.
* A set of comment-files.

There are two ways this software is typically used:

* On a single host.
   * The blog input is stored upon your web-server and you generate the output directly to a HTTP-accessible directory upon that machine.
* With multiple hosts.
  *  The blog input lives upon one machine, and once you've generated the output you copy it over to a remote web-server where it may be viewed.

Depending upon which of these ways you use the software the comment support will need to be handled differently.



# Common Setup

Install the included file `cgi-bin/comments.cgi` upon the web-server which hosts the blog, and adjust the settings at the start of that file to specify the basic configuration:

* The local directory to save the comments within.

From here the configuration varies depending on how you're going to run the software.

The purpose of the script is to receive the comments, via the POSTed FORM in the individual blog-posts, and save them to disk, locally.  The **crucial** thing is that the posts have appropriate filenames, such that they can be appended to the correct entry when the blog is rebuilt.

Once you've installed the comment CGI script you should update your `ephemeris.json` configuration file to contain the URL:

    {
      ...
      "CommentAPI": "http://example.com/cgi-bin/comments.cgi",
      ..
    }


# Single Machine

If you have only a single machine then you may configure the `comments.cgi` script to save the comments in text files directly within your blog tree.

Assuming you have something like this:

* `comments/`
   * The directory to contain the comments.
* `data/`
   * The directory where your blog posts are loaded from.

You may then regenerate your blog via:

     ephemeris

Where you'd configure `ephemeris.json` to read:

     {
       "PostsPath":    "./data/",
       "CommentsPath": "./comments/",
       "Prefix":       "http://my.blog.site/",
       "CommentAPI":   "http://my.blog.site/cgi-bin/comments.cgi"
     }

This will ensure that the comments saved by your web-server into the comments directory are included in the (re)generated blog.



# Multiple Machines

If you have the blog input files upon machine "`local`" and the hosted blog upon the machine "`remote`" then you will run into problems:

* The comments are saved by your web-server to a local directory upon the machine "`remote`".
* To rebuild the blog upon your local machine, "`local`", you must have those files.

The solution is to generate your blog in a three-step process:

1. Copy the comment files, if any, from "remote" to "local".
2. Rebuild the blog.
3. Upload the generated blog.

I'd recommend using `rsync` for steps 1 & 3.

Something like this:

      rsync -vazr remote:/srv/comments ./comments/
      ephemeris
      rsync -vazr ./output remote:/var/www/blog/
