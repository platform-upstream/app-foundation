# syntax=docker/dockerfile:1.29@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# The module's checks (ci/check.sh) as a build: a platform's CI runs it as the verify build of
# every pull request (status ci/verify). Nothing is pushed; the image is thrown away.
FROM docker.io/library/golang:1.27@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends nodejs npm >/dev/null && rm -rf /var/lib/apt/lists/* \
 && npm install -g pnpm@12 >/dev/null
WORKDIR /src
COPY . .
RUN git config --global --add safe.directory /src && ci/check.sh
