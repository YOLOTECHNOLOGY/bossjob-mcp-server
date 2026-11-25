#!/usr/bin/env sh

set -e

start_time=$(date +%s)

ENV=local go test -failfast -gcflags="all=-l -N" -count=1 -run ^TestCI_ -coverprofile=c.out -coverpkg=./... ./tests/...
go tool cover -func=c.out | grep -Ev '\s0\.0%$' | grep -v total | sort -k3 -nr | awk '
  {
    print
    v = $3
    sub(/%$/, "", v)
    cov[++n] = v + 0
  }
  END {
    if (n <= 0) {
      print "P90 coverage (non-zero funcs): N/A"
      exit 0
    }
    pos = n * 0.9
    idx = int(pos)
    if (pos > idx) idx++
    if (idx < 1) idx = 1
    if (idx > n) idx = n
    printf("P90 coverage (non-zero funcs): %.1f%%\n", cov[idx])
  }
'
go tool cover -func=c.out | grep total

end_time=$(date +%s)
echo "Execution time: $((end_time - start_time)) seconds"
