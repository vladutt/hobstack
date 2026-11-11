# macOS signing and notarization (GitHub releases)

GitHub release builds for macOS (the `mac` job in `.github/workflows/release.yml`) are signed with your
Developer ID certificate and notarized by Apple, so Gatekeeper opens the downloaded `.dmg` / `.app` without warnings.

Local builds (`scripts/build-mac.sh <version>`) don't need any of this: without `MACOS_SIGN_IDENTITY` they are
signed ad-hoc and not notarized, the same as before. Until the secrets below exist, release builds are ad-hoc too.

## One-time setup

1. **Export the certificate.** Open Keychain Access, go to *login* > *My Certificates*, and find
   `Developer ID Application: Ilie Vladut Teodor (97NL489453)`. Expand it so the private key shows, select the
   certificate *and* its key, then choose *File > Export Items…* and save as `cert.p12` with a strong export password.

2. **Encode it** (the secret has to be text):

   ```sh
   base64 -i cert.p12 | pbcopy
   ```

3. **Create an app-specific password** at https://appleid.apple.com > *Sign-In and Security* > *App-Specific Passwords*.
   Apple's notary service uses it, not your normal Apple ID password.

4. **Add the 6 secrets** to the GitHub repository (*Settings > Secrets and variables > Actions*), or with `gh`:

   | Secret | Value |
   |---|---|
   | `MACOS_CERT_P12` | the base64 text from step 2 |
   | `MACOS_CERT_PASSWORD` | the export password from step 1 |
   | `MACOS_SIGN_IDENTITY` | `Developer ID Application: Ilie Vladut Teodor (97NL489453)` |
   | `APPLE_ID` | your Apple ID e-mail |
   | `APPLE_TEAM_ID` | `97NL489453` |
   | `APPLE_APP_PASSWORD` | the app-specific password from step 3 |

   ```sh
   pbpaste | gh secret set MACOS_CERT_P12
   gh secret set MACOS_CERT_PASSWORD
   gh secret set MACOS_SIGN_IDENTITY --body "Developer ID Application: Ilie Vladut Teodor (97NL489453)"
   gh secret set APPLE_ID
   gh secret set APPLE_TEAM_ID --body "97NL489453"
   gh secret set APPLE_APP_PASSWORD
   ```

   (`gh secret set NAME` without `--body` asks for the value, so it doesn't end up in your shell history.)
   Then delete `cert.p12` and clear the clipboard.

5. **Verify after the next release.** Tag a release as usual. In the `mac` job log you should see
   `notarization of HobStack-<v>.dmg: Accepted`, the same for `hobstack-bins.zip`, and the two Gatekeeper checks
   (`source=Notarized Developer ID`). Then download the `.dmg` from the release page in a browser and open it:
   there should be no "unidentified developer" warning. You can also check it from the terminal:

   ```sh
   spctl -a -t open --context context:primary-signature -v ~/Downloads/HobStack-<v>.dmg
   codesign -dv --verbose=2 /Applications/HobStack.app   # Authority=Developer ID Application: ...
   ```

## How it fails

- `MACOS_CERT_P12` is missing: the job builds ad-hoc and succeeds (forks, or before setup).
- The certificate is there but `MACOS_SIGN_IDENTITY` doesn't match it: the import step fails.
- `MACOS_SIGN_IDENTITY` is set but `APPLE_ID`, `APPLE_TEAM_ID` or `APPLE_APP_PASSWORD` is missing: the build stops
  with a message that names the missing values. Notarization is never skipped silently.
- Apple rejects the upload: the job prints the notary log and fails.

## In-app updates

The in-app updater replaces the binary inside `HobStack.app` and re-signs the bundle ad-hoc. That's fine: files the app
downloads itself are not quarantined, so Gatekeeper doesn't check them again.
