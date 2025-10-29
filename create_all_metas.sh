#!/bin/bash

# Script to create .meta files for all test files

# Find all .txt files in shared directory (excluding .meta files)
for file in shared/*.txt; do
    # Skip if no .txt files found
    if [ ! -f "$file" ]; then
        echo "No .txt files found in shared/"
        exit 1
    fi
    
    # Skip if it's already a .meta file
    if [[ "$file" == *.meta ]]; then
        continue
    fi
    
    echo "Creating .meta for $file..."
    go run createmeta.go "$file"
    echo ""
done
