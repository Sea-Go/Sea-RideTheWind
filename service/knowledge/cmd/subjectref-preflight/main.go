// subjectref-preflight audits the v1 Knowledge tables before a SubjectRef v2 migration.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	os.Exit(run())
}

func run() int {
	output := flag.String("report", "", "write the JSON report to this path (default: stdout)")
	timeout := flag.Duration("timeout", 2*time.Minute, "maximum audit duration")
	flag.Parse()
	if *timeout <= 0 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: subjectref-preflight [-report path] [-timeout duration]")
		return 2
	}
	dsn := os.Getenv("KNOWLEDGE_PREFLIGHT_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "KNOWLEDGE_PREFLIGHT_DSN is required")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect failed; verify the local target and credentials")
		return 1
	}
	defer conn.Close(context.Background())
	report, err := preflight(ctx, conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "preflight failed:", err)
		return 1
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode report failed:", err)
		return 1
	}
	data = append(data, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = writeReport(*output, data)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "write report failed:", err)
		return 1
	}
	hash := sha256.Sum256(data)
	fmt.Fprintln(os.Stderr, "report_sha256="+hex.EncodeToString(hash[:]))
	if report.Blocking {
		return 3
	}
	return 0
}

func writeReport(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	return nil
}
