package shell

import (
	"os/exec"
)

// WaitStream starts cmd, merges stderr into stdout, and calls onChunk for each
// read. It returns the combined output and Wait's error.
func WaitStream(cmd *exec.Cmd, onChunk func(string)) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var output []byte
	buf := make([]byte, 4096)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			output = append(output, buf[:n]...)
			if onChunk != nil {
				onChunk(string(buf[:n]))
			}
		}
		if readErr != nil {
			break
		}
	}
	return output, cmd.Wait()
}
