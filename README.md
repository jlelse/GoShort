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

## Authentication

The preferred authentication method is Basic Authentication. If you try to create, modify or delete a short link, in the browser a popup will appear asking for username and password. Enter just the password you configured. Alternatively you can append a URL query parameter `password` with your configured password.

## Create a new short link

To create a new short link, call "`shortUrl` + `/s?url=` + URL to shorten". If you want, you can append `&slug=` with the preferred slug.

## Update a short link

To update a short link, call "`shortUrl` + `/d?slug=` + slug to update + `&new=` + new long URL"

## Delete a short link

To delete a short link, call "`shortUrl` + `/d?slug=` + slug to delete".

## License

GoShort is licensed under the MIT license, so you can do basically everything with it, but nevertheless, please contribute your improvements to make GoShort better for everyone. See the LICENSE file.