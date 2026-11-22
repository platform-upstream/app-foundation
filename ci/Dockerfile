# syntax=docker/dockerfile:1.29@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# The module's checks (ci/check.sh) as a build: a platform's CI runs it as the verify build of
# every pull request (status ci/verify). Nothing is pushed; the image is thrown away.
# The browser tests (TestPhoneAudit, TestModulesInBrowser skip without a browser): the registry's
# phone audit and UI modules in Playwright's Chromium. Same Playwright as the scaffold's phone stage.
FROM mcr.microsoft.com/playwright:v1.65.0-noble@sha256:9beb546099e11f52d62e505537914870e01a67e0e6b00ae41da4775324e73dbe AS browser
WORKDIR /pw
RUN npm install --no-save --no-package-lock --no-audit --no-fund playwright@1.65.0 >/dev/null
WORKDIR /src
COPY frontendregistry/ ./frontendregistry/
RUN cd frontendregistry && export PHONE_AUDIT_PLAYWRIGHT=/pw/node_modules/playwright && \
    node testdata/phone_audit_test.mjs sources/phone-audit/phone-audit.ts && \
    node testdata/modules_browser_test.mjs sources && touch /browser.ok

FROM docker.io/library/golang:1.27@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends nodejs npm >/dev/null && rm -rf /var/lib/apt/lists/* \
 && npm install -g pnpm@12 >/dev/null
WORKDIR /src
COPY --from=browser /browser.ok /browser.ok
COPY . .
RUN git config --global --add safe.directory /src && ci/check.sh
