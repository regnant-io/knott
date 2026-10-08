# Remove Co-Authored-By: Claude from commit messages
$input | Where-Object { $_ -notmatch 'Co-Authored-By.*Claude' -and $_ -notmatch 'Co-Authored-By.*claude' }
