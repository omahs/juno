# Note

This folder is just being used to gather the necessary components for generating PIE files from Juno blocks. 

These PIE files can be sent to the SHARP endpoint for proving. If we want to use the Stone-prover we should also be able to get the neccessary arguments from cairo-run.

This is very much an active work in progress, so the commands etc may not run correctly etc.

## Requirements

1. cairo installed on your system.
2. A valid `program_inputs.json` file.

## Instructions

To run the `os.cairo` program, you need to compile it first. After that, you can use the `cairo-run` command with the appropriate flags.

The steps should be something like this:

1. Compile the `os.cairo` file:

```bash
cairo-compile os.cairo
```

2. Run the compiled program with a valid program_inputs.json file:
```bash
cairo-run --program os.cairo --program_input program_inputs.json --cairo_pie_output pie.json
```
This will output the result in a pie.json file.

