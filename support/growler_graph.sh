#!/bin/sh

# Given a log file from a growler run and the number a specific downloader to
# look at (0 always works) graphs out the stats that have been output for that
# downloader using gnuplot

if [ "$2" == "" ]; then
    echo "Usage: $0 <log file> <downloader number>"
    exit 1
fi

cat "$1" | grep "STAT" \
         | grep ": $2" \
         | awk 'BEGIN{i=0} {print i++, $5, $6, $7, $8}' \
         | sed 's/[^ ]\+://g' \
         | sed 's/ /\t/g' \
         > /tmp/growler_${2}_cols.log

gnuplot -persist <<EOF
    plot '/tmp/growler_${2}_cols.log' using 1:2 with lines title 'Total', \
         '/tmp/growler_${2}_cols.log' using 1:3 with lines title 'Gets', \
         '/tmp/growler_${2}_cols.log' using 1:4 with lines title 'Heads', \
         '/tmp/growler_${2}_cols.log' using 1:5 with lines title 'Excluded'
EOF
