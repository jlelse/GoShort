# GoShort

GoShort is a simple URL shortener written in Go. It's minimal and only uses a simple SQLite database in the background.

## Configuration

Configuration can be done with a simple `config.{json|yaml|toml}` file in the working directory or a subdirectory `config`.

These are the required config values:

* `password`: Password to create or delete short links
* `shortUrl`: The short base URL (without trailing slash!)
* `defaultUrl`: The default URL to which should be redirected when no slug is specified

These are optional config values:

* `dbPath`: Relative path where the database should be saved

See the `example-config.yaml` file for an example configuration.

## Create a new short link

To create a new short link, open "`shortUrl` + `/s?url=` + URL to shorten" in the browser. If you want, you can append `&slug=` with the preferred slug.

## Delete a short link

To delete a short link, open "`shortUrl` + `/d?slug=` + slug to delete" in the browser.

## License

GoShort is licensed under the MIT license, so you can do basically everything with it, but nevertheless, please contribute your improvements to make GoShort better for everyone. See the LICENSE file.