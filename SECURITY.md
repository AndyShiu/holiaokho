# Security Policy

## Reporting a vulnerability

**Please do not open a public issue.**

Use GitHub's private vulnerability reporting: go to the **Security** tab of
this repository and choose *Report a vulnerability*. The report stays private
between you and the maintainers until a fix is available.

Useful things to include, if you have them: what an attacker gains, the
smallest set of steps that reproduces it, and which version you tested.

You will get an acknowledgement within a few days. This is a small project —
there is no 24-hour security desk — but reports are read and taken seriously.

## Why this matters here

Holiaokho is an artifact repository. It sits between a team's build system and
the packages that build depends on, which puts it inside their software supply
chain. A flaw that lets someone serve the wrong bytes for a package, or read
artefacts they should not see, reaches much further than the server itself.

Bugs of that shape are treated as serious regardless of how a severity
calculator scores them.

## Supported versions

The latest release is the supported one. Fixes land there first; older
versions are not backported.

## Scope

In scope:

- Authentication or authorisation bypass
- Serving incorrect or attacker-controlled content for a package
- Reading artefacts or credentials across permission boundaries
- Remote code execution, path traversal, SSRF through proxy configuration
- Leaking stored upstream credentials

Not in scope:

- Findings that require an account already holding the permission in question
- Denial of service through sheer request volume (rate limiting is a known gap)
- Anything about a deployment's own configuration — a repository someone made
  public on purpose, or a reverse proxy that terminates TLS the wrong way

## Disclosure

Once a fix is released, the report becomes public and you get credit for it
unless you would rather not be named.
