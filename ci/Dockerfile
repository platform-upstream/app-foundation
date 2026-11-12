# syntax=docker/dockerfile:1.7
# The module's checks (ci/check.sh) as a build: a platform's CI runs it as the verify build of
# every pull request (status ci/verify). Nothing is pushed; the image is thrown away.
FROM docker.io/library/golang:1.27
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends nodejs npm >/dev/null && rm -rf /var/lib/apt/lists/* \
 && npm install -g pnpm@12 >/dev/null
WORKDIR /src
COPY . .
RUN git config --global --add safe.directory /src && ci/check.sh
