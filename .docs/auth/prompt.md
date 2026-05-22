Use the spec-writer skill workflow to create `.docs/auth/spec.md`, to specify the addition of authentication and access control to the frontend.

The backend server should use LDAP to verify a user is who they say they are. Follow https://github.com/wtsi-hgi/ibackup/blob/master/cmd/server.go on how it works with LDAP; specify using the same command line options. The server will have to be https-only with LDAP auth activated, and the frontend will have to talk to the backend securely and with a self-signed cert in dev mode.

Once we know the username of the logged in user, access control should work along these lines, where the user can view a result set page if:

- They are the requester or operator of the result set
- They belong to the same unix group that owns the result set output directory

If we do not currently capture the gid of output directories during result set registration, this will need to be spec’d, with a database schema update (as opposed to doing live file system checks each time, which would be too slow). (For backwards compatability, a null gid should be treated as no one having access; an admin will have to populate the gid column manually.)
All users (including users who have not logged in yet) will be able to search for and see search results for all registered results sets, but the search results and listing of latest result sets will show a “locked” icon, be greyed out and not be clickable if they don’t have access. Direct url access to result set pages they don’t have access to will also just show a locked symbol with a link back to the front page.

The top right of the page will have a compact login/logout tool that displays the currently logged in user. Search shadcn/ui components for new UI elements.

There needs to be a secure system in place that allows the user who started the backend server, and only that user, to do registrations on the command line (`wa results register`), without needing to enter a password. Use the “server token file” system of https://github.com/wtsi-hgi/go-authserver for this. Also allow other users to use `wa results register` which will ask them for their ldap password if they don’t have a token in their XDG_STATE_HOME, again following the go-authserver way of doing things. Other users using `register` will have their registrations forced to have “operator” set to their own username.
Emedding go-authserver in our server implies also spec’ing a full switch from Chi to Gin, involving migratation of results routes/tests to Gin conventions etc. This means we also get to use go-authserver’s JWT issuing, refresh, https and cert handling and have the option in the future of doing okta auth. So much of the spec and UATs may be concerned with the switch to Gin, since a lot of auth-related requirements get satisfied by using the go-authserver implementation.

## Notes

The frontend should authenticate through Next.js server-side handlers/actions that proxy login, logout, and refresh operations to the Go backend and manage HTTP-only cookies for browser clients.

Search, latest, and list responses should continue returning all registered result sets, with an access or locked field that lets the frontend grey out inaccessible rows and make them non-clickable.

Backend authorization should protect result detail, file listing, file content, and mutation endpoints. Public search, public listing/latest, stats, and metadata-key endpoints should remain accessible to unauthenticated users.

Unix group membership checks should use go-authserver's authenticated user model, including `User.GIDs()` and other relevant methods, to compare the user's group ids to the gid stored for the result set output directory.

For `wa results register`, the server-owner token may set any requester or operator. Users who authenticate with LDAP/password instead of the server-owner token must have the registration's operator forced to their authenticated username.

Non-registration mutation endpoints should remain server-owner-token-only for this feature. LDAP-authenticated users may register result sets, but delete, rescan, and any other broader mutation permissions are out of scope unless they already map directly to registration.

For LDAP/password registration, only the operator is forced to the authenticated username. The requester remains user-supplied.

Served backend modes must require HTTPS and LDAP authentication. Tests may use explicit fake authentication/mocks, but real dev servers should still run securely with self-signed development certificates.

Protected result detail and file APIs should return a stable locked/forbidden JSON response when access is denied so the frontend can render the locked state. The frontend page for an inaccessible direct URL should show only the locked symbol and a link back to the front page.

In development, the Next.js server-side backend client should trust the Go backend's self-signed certificate through an explicit development CA/certificate path configuration. It should not disable TLS verification globally.

Authorization comparisons against result set requester and operator should use the canonical username returned by go-authserver for the authenticated LDAP user.

The WA server token and client/user token storage should follow go-authserver's server-token-file system and default server/user token directory conventions, with a WA-specific token basename appropriate for the CLI.

After adopting go-authserver, `wa results serve` should expose HTTPS and certificate command-line options following the ibackup/go-authserver style, replacing the old plain HTTP port-only shape where necessary. The spec should call out any intended compatibility handling for existing users.

Development TLS trust between Next.js and the Go backend should use an explicit environment variable for the backend CA/certificate path, for example `WA_RESULTS_BACKEND_CA_CERT`.

If required LDAP options are missing in any non-test server mode, backend startup should fail instead of falling back to passwordless or fake authentication.

For legacy rows where the output directory gid is null, the result set should be treated as inaccessible to everyone through normal user authorization, even if requester or operator matches. An administrator must populate the gid to make normal access checks succeed.

The backend server should capture and persist the output directory gid during registration, by statting the registered output directory on the server side. New registrations should fail if the server cannot determine the output directory gid, rather than trusting an arbitrary client-supplied gid.

Existing CLI read commands that call protected result detail or file endpoints should use the same go-authserver token/password flow as registration.

Switching `wa results serve` to go-authserver HTTPS/auth is an intentional breaking change for real served modes: plain HTTP serving should be removed or rejected outside explicit test-only paths.

The Next.js frontend should also run over HTTPS in development so browser auth cookies can remain `Secure` in dev, test, and production. Use self-signed development certificates for both frontend and backend dev servers.
