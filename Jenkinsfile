pipeline {
    agent any

    options {
        timestamps()
        disableConcurrentBuilds(abortPrevious: true)
    }

    environment {
        PROJECT_NAME = 'mail'
        GO_CACHE = "${WORKSPACE}@tmp/go-mod-cache"
        GO_BUILD = "${WORKSPACE}@tmp/go-build-cache"
    }

    stages {
        stage('Build information') {
            steps {
                script {
                    if (env.TAG_NAME) {
                        env.VERSION = env.TAG_NAME.replaceFirst(/^v/, '')
                    } else {
                        env.VERSION = sh(script: 'git rev-parse --short=7 HEAD', returnStdout: true).trim()
                    }
                    env.COMMIT_ID = sh(script: 'git rev-parse --short=7 HEAD', returnStdout: true).trim()
                    env.BUILD_EPOCH = sh(script: 'date -u +%s', returnStdout: true).trim()
                    env.INTERNAL_NAME = sh(script: "sed -n 's/^internal_name:[[:space:]]*//p' IDEA.md | head -n1 | tr -d '[:space:]'", returnStdout: true).trim()
                    if (!env.INTERNAL_NAME) {
                        error('IDEA.md must define internal_name')
                    }
                    env.OFFICIAL_SITE = fileExists('site.txt') ? readFile('site.txt').trim() : (env.OFFICIAL_SITE ?: '')
                }
            }
        }

        stage('Security') {
            parallel {
                stage('Secret scan') {
                    steps {
                        sh '''
                            set -eu
                            if [ -n "${CHANGE_TARGET:-}" ]; then
                                base=$(git merge-base "origin/$CHANGE_TARGET" HEAD)
                                container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                                docker run --rm --no-healthcheck --name "$container_name" --memory=2g --cpus=2 \
                                    -v "$WORKSPACE:/work" trufflesecurity/trufflehog:3.95.9 git file:///work \
                                    --since-commit "$base" --branch HEAD --results=verified,unknown --fail --fail-on-scan-errors
                            else
                                for result_set in verified unverified,unknown; do
                                    container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                                    args="--results=$result_set"
                                    [ "$result_set" != verified ] || args="$args --fail"
                                    docker run --rm --no-healthcheck --name "$container_name" --memory=2g --cpus=2 \
                                        -v "$WORKSPACE:/work" trufflesecurity/trufflehog:3.95.9 git file:///work $args --fail-on-scan-errors
                                done
                            fi
                        '''
                    }
                }
                stage('Workflow policy') {
                    steps {
                        sh '''
                            set -eu
                            bad=$(grep -RnE '^[[:space:]]*uses:' .github/ .gitea/ .forgejo/ 2>/dev/null | grep -vE '@[0-9a-f]{40}([[:space:]]|$)' || true)
                            if [ -n "$bad" ]; then printf '%s\\n' "$bad"; exit 1; fi
                        '''
                    }
                }
                stage('Vulnerability scan') {
                    when {
                        expression { fileExists('go.sum') }
                    }
                    steps {
                        sh '''
                            set -eu
                            container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                            docker run --rm --no-healthcheck --name "$container_name" \
                                --memory=4g --cpus=2 -v "$WORKSPACE:/app" -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
                                -w /app -e GOFLAGS=-buildvcs=false casjaysdev/go:latest govulncheck ./...
                        '''
                    }
                }
                stage('Image scan') {
                    when {
                        expression { fileExists('docker/Dockerfile') }
                    }
                    steps {
                        sh '''
                            set -eu
                            docker build -t ghcr.io/apimgr/mail:jenkins-scan -f docker/Dockerfile .
                            container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                            docker run --rm --no-healthcheck --name "$container_name" --entrypoint sh ghcr.io/apimgr/mail:jenkins-scan \
                                -c 'for tool in bash curl tor i2pd tini; do command -v "$tool" || exit 1; done'
                            scan_dir=$(mktemp -d "${TMPDIR:-/tmp}/apimgr/mail-XXXXXX")
                            trap 'rm -rf "$scan_dir"' EXIT
                            docker save ghcr.io/apimgr/mail:jenkins-scan -o "$scan_dir/image.tar"
                            container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                            timeout -k 30 3600 docker run --rm --no-healthcheck --name "$container_name" \
                                --memory=2g --cpus=2 -v "$scan_dir:/scan" aquasec/trivy:0.76.2 image \
                                --input /scan/image.tar --exit-code 1 --severity CRITICAL,HIGH
                        '''
                    }
                }
            }
        }

        stage('Quality') {
            parallel {
                stage('Lint') {
                    steps {
                        sh '''
                            set -eu
                            container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                            docker run --rm --no-healthcheck --name "$container_name" \
                                --memory=4g --cpus=2 -v "$WORKSPACE:/app" -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
                                -v "$GO_BUILD:/usr/local/share/go/cache" -w /app -e GOFLAGS=-buildvcs=false \
                                casjaysdev/go:latest sh -c 'go vet ./... && staticcheck ./...'
                        '''
                    }
                }
                stage('Test') {
                    steps {
                        sh '''
                            set -eu
                            container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                            docker run --rm --no-healthcheck --name "$container_name" \
                                --memory=4g --cpus=2 -v "$WORKSPACE:/app" -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
                                -v "$GO_BUILD:/usr/local/share/go/cache" -w /app -e GOFLAGS=-buildvcs=false \
                                -e CGO_ENABLED=0 casjaysdev/go:latest sh -c 'set -eu; \
                                    mkdir -p "${TMPDIR:-/tmp}/apimgr"; \
                                    coverage_dir=$(mktemp -d "${TMPDIR:-/tmp}/apimgr/mail-XXXXXX"); \
                                    trap "rm -rf \"$coverage_dir\"" EXIT; \
                                    go generate ./src/graphql; \
                                    mkdir -p "$coverage_dir/data"; \
                                    TMPDIR="$coverage_dir" go test -cover ./... -args -test.gocoverdir="$coverage_dir/data"; \
                                    go tool covdata textfmt -i="$coverage_dir/data" -o="$coverage_dir/coverage.out"; \
                                    coverage=$(go tool cover -func="$coverage_dir/coverage.out" | awk "/^total:/ {gsub(\"%\", \"\", \$3); print int(\$3)}"); \
                                    echo "Coverage: $coverage%"; \
                                    test "$coverage" -ge 60
                        '''
                    }
                }
            }
        }

        stage('Build') {
            when {
                not { changeRequest() }
            }
            steps {
                sh '''
                    set -eu
                    mkdir -p binaries
                    container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                    docker run --rm --no-healthcheck --name "$container_name" \
                        --memory=4g --cpus=2 -v "$WORKSPACE:/app" -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
                        -v "$GO_BUILD:/usr/local/share/go/cache" -w /app -e CGO_ENABLED=0 \
                        -e GOFLAGS=-buildvcs=false -e VERSION="$VERSION" -e COMMIT_ID="$COMMIT_ID" \
                        -e BUILD_EPOCH="$BUILD_EPOCH" -e OFFICIAL_SITE="$OFFICIAL_SITE" \
                        -e INTERNAL_NAME="$INTERNAL_NAME" casjaysdev/go:latest sh -c ' \
                            set -eu; \
                            go generate ./src/graphql; \
                            for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 freebsd/amd64 freebsd/arm64; do \
                                os=${platform%/*}; arch=${platform#*/}; ext=; [ "$os" = windows ] && ext=.exe; \
                                LDFLAGS="-s -w -X 'main.Version=$VERSION' -X 'main.CommitID=$COMMIT_ID' -X 'main.BuildEpoch=$BUILD_EPOCH' -X 'main.OfficialSite=$OFFICIAL_SITE' -X 'main.internalName=$INTERNAL_NAME'"; \
                                GOOS=$os GOARCH=$arch go build -buildvcs=false -trimpath -ldflags "$LDFLAGS" \
                                    -o "binaries/mail-$os-$arch$ext" ./src; \
                                GOOS=$os GOARCH=$arch go build -buildvcs=false -trimpath -ldflags "$LDFLAGS" \
                                    -o "binaries/mail-cli-$os-$arch$ext" ./src/client; \
                            done'
                '''
            }
        }

        stage('Release artifacts') {
            when {
                tag pattern: 'v*', comparator: 'GLOB'
            }
            steps {
                sh '''
                    set -eu
                    tar --exclude='.git' --exclude='.github' --exclude='.gitea' --exclude='.forgejo' \
                        --exclude='binaries' --exclude='releases' --exclude='*.tar.gz' \
                        -czf "binaries/mail-$VERSION-source.tar.gz" .
                    echo "$VERSION" > binaries/version.txt
                    container_name="mail-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"
                    docker run --rm --no-healthcheck --name "$container_name" --memory=4g --cpus=2 \
                        -v "$WORKSPACE:/app" -v "$GO_CACHE:/usr/local/share/go/pkg/mod" -w /app \
                        -e GOFLAGS=-buildvcs=false casjaysdev/go:latest \
                        cyclonedx-gomod mod -json -output "binaries/mail-$VERSION-sbom.cdx.json"
                    files=$(find binaries -maxdepth 1 -type f -printf '%f\n' | sort)
                    (cd binaries && sha256sum $files > sha256.txt && sha512sum $files > sha512.txt)
                '''
                archiveArtifacts artifacts: 'binaries/*', fingerprint: true
            }
        }
    }
}
