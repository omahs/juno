# courtesy of chatgpt

BEGIN {
    count = 0   # Initialize the error count to 0
}

/ERROR/ {       # If the line contains "ERROR"
    error = 1     # Set the error flag to true
    buffer = $0   # Store the line in the buffer
    count++       # Increment the error count
    next          # Skip to the next line
}

error && !/INFO/ {  # If the error flag is true and the line does not contain "INFO"
    buffer = buffer "\n" $0   # Append the line to the buffer
    next                      # Skip to the next line
}

error && /INFO/ {   # If the error flag is true and the line contains "INFO"
    print buffer      # Print the buffer (contains all lines with "ERROR" up to "INFO")
    error = 0         # Reset the error flag to false
    buffer = ""       # Reset the buffer to an empty string
}

END {
    print "Total number of errors:", count  # Print the total number of errors
}
