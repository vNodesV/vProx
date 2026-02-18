#!/bin/bash
ts=$(date -u +%m%d%y_%H%MUTC)
source=~/vApp/modules/vProx/logs
log="main.log"
cpTarget="main.log.$ts"
archiveTarget="main.log.$ts.tar.gz"
archiveFolder="$HOME/vProx/logs"

cp $source/$log $source/$cpTarget
truncate -s 0 $source/$log
tar -czf $source/$cpTarget.tar.gz $source/$cpTarget
mv $source/$cpTarget.tar.gz $archiveFolder

echo "Archiving $archiveTarget to $archiveFolder Completed"
