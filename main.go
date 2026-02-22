package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

type Cookie struct {
	RevelSession string `json:"REVEL_SESSION"`
	Ga           string `json:"_ga"`
	Ga_          string `json:"_ga_RC512FD18N"`
	TimeDelta    string `json:"timeDelta"`
	RevelFlash   string `json:"REVEL_FLASH"`
}

type cookieContextKey struct{}

var (
	getProblemsPageURL = "https://atcoder.jp/contests/abc"
	testcasesRegex     = `(?s)<h3>Sample Input\s*\d+</h3>\s*<pre>(.*?)</pre>.*?<h3>Sample Output\s*\d+</h3>\s*<pre>(.*?)</pre>`
	cookieKey          = cookieContextKey{}
)

func getCookieFilePath() (string, error) {
	// Check environment variable first
	if envPath := os.Getenv("AC_COOKIE_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
		return "", fmt.Errorf("AC_COOKIE_PATH set to %q but file not found", envPath)
	}

	// Check XDG config directory (~/.config/ac/cookie.json)
	if homeDir, err := os.UserHomeDir(); err == nil {
		configPath := filepath.Join(homeDir, ".config", "ac", "cookie.json")
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	// Fall back to current directory
	if _, err := os.Stat("cookie.json"); err == nil {
		return "cookie.json", nil
	}

	return "", fmt.Errorf("cookie.json not found. Place it in ~/.config/ac/cookie.json or set AC_COOKIE_PATH")
}

const defaultCPPTemplate = `#include <iostream>
#include <iostream>
#include <vector>
#include <algorithm>
#include <string>
#include <queue>
#include <stack>
#include <set>
#include <map>
#include <unordered_map>
#include <unordered_set>
#include <cmath>
#include <numeric>
#include <tuple>
#include <cassert>
using namespace std;

using P = pair<int,int>;
using ll = long long;
#define all(x) x.begin(), x.end()
#define MOD 1000000007
#define rep(i,n) for (int i = 0; i < (n); ++i)

inline void test_case() {


}

signed main()
{
    ios::sync_with_stdio(0);
    cin.tie(0);
    int test_case_number = 1;
    // cin >> test_case_number;
    while (test_case_number--)
        test_case();
    return 0;
}
`

func main() {

	ctx := context.Background()

	rootCmd := &cobra.Command{
		Use:   "ac",
		Short: "CLI tool for downloading atcoder problems and tests and also running tests for it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	rootCmd.AddCommand(downloadCmd(ctx))
	rootCmd.AddCommand(runTestsCmd(ctx))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func downloadCmd(ctx context.Context) *cobra.Command {

	var contestNumber int
	var directoryPath string

	downloadCmd := &cobra.Command{
		Use:   "dw",
		Short: "Used for downloading problems and test with them in specilized directories",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {

			// Check os
			switch os := runtime.GOOS; os {
			case "darwin":
				fmt.Println("Hi from darwin side")
			}

			// Find and open cookie file
			cookiePath, err := getCookieFilePath()
			if err != nil {
				return err
			}

			file, err := os.OpenFile(cookiePath, os.O_RDONLY, 0400)
			if err != nil {
				return err
			}

			defer func() {
				if err := file.Close(); err != nil {
					log.Fatal(err)
				}
			}()

			// Read that shit in a buffer
			body, err := io.ReadAll(file)
			if err != nil {
				return err
			}

			// Init a struct, read the body there. Thats why we have json tags in struct
			var cookie Cookie
			err = json.Unmarshal(body, &cookie)
			if err != nil {
				return err
			}

			// If successful, store it in context as a value and get the value from context when required
			ctx = context.WithValue(ctx, cookieKey, cookie)

			// Get current working directory
			currDir, err := os.Getwd()
			if err != nil {
				return err
			}

			// Default directory path to current dir when flag not provided
			if directoryPath == "" {
				directoryPath = currDir
			} else if !filepath.IsAbs(directoryPath) {

				// Check if the directory actually exists, or else default to curr directory
				_, err := os.Stat(directoryPath)
				if err != nil {

					if errors.Is(err, os.ErrNotExist) {
						return fmt.Errorf("directory '%s' does not exists: %v", directoryPath, err)
					} else {
						return fmt.Errorf("error checking directory '%s': %v", directoryPath, err)
					}
				}

			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			// Download the html files, look for testcases, create new directories, set them up
			// We have the contest number with us, send it now in functions that will take care of it

			err := DownloadAndLoadProblems(ctx, contestNumber, directoryPath)

			return err
		},
	}

	downloadCmd.Flags().IntVarP(&contestNumber, "contest", "c", 1, "used to give contest number")
	downloadCmd.Flags().StringVarP(&directoryPath, "path", "p", "", "used to express directory path (defaults to current directory)")

	return downloadCmd
}

func runTestsCmd(ctx context.Context) *cobra.Command {

	runTestsCmd := &cobra.Command{
		Use:   "jj [paths...]",
		Short: "Run testcases for one or more problem directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default to current directory when no path is provided
			if len(args) == 0 {
				args = []string{"."}
			}

			var targetDirs []string
			seen := make(map[string]struct{})

			for _, p := range args {
				dirs, err := resolveTestTargets(p)
				if err != nil {
					return err
				}

				for _, d := range dirs {
					if _, exists := seen[d]; exists {
						continue
					}
					seen[d] = struct{}{}
					targetDirs = append(targetDirs, d)
				}
			}

			if len(targetDirs) == 0 {
				return fmt.Errorf("no test directories found")
			}

			var errs []string

			for _, dir := range targetDirs {
				fmt.Printf("==> Running tests in %s\n", dir)
				if err := runTestsInDir(ctx, dir); err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", dir, err))
				}
			}

			if len(errs) > 0 {
				return fmt.Errorf(strings.Join(errs, "\n"))
			}

			return nil
		},
	}

	return runTestsCmd
}

func DownloadAndLoadProblems(ctx context.Context, contestNumber int, directoryPath string) error {

	// First check how many problems are there, then proceed to download their data(html), parse it, and create a A.cpp file and multiple testcases file in their respective directories
	problemsNumber, err := GetNumberOfProblems(ctx, contestNumber)
	if err != nil {
		return err
	}

	// Now we will have to make that many directories, starting from A and so on. Lets take this directory as a flag
	// Loop over the problems and create directoriess.
	// First build the main directory i.e. the contest number
	directoryPath = filepath.Join(directoryPath, fmt.Sprintf("ABC%d", contestNumber))
	err = os.MkdirAll(directoryPath, 0755)
	if err != nil {
		return err
	}

	for i := 0; i < problemsNumber; i++ {
		alpha := string('A' + rune(i))

		newProblemDirectory := filepath.Join(directoryPath, alpha)
		err := os.MkdirAll(newProblemDirectory, 0755)
		if err != nil {
			return err
		}
	}

	// Directories are created, now copy the main.cpp file and add testcases in the directory
	// Download the html file of the contest page, parse the input and output and create new files
	errChan := make(chan error, problemsNumber)

	var wg sync.WaitGroup

	for i := 0; i < problemsNumber; i++ {
		alpha := string('A' + rune(i))

		wg.Add(1)
		go func(a string) {
			downloadAndCreateTestcases(ctx, contestNumber, directoryPath, a, &wg, errChan)
		}(alpha)
		time.Sleep(200 * time.Millisecond)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

func GetNumberOfProblems(ctx context.Context, contestNumber int) (int, error) {

	currentContestPageURL := getProblemsPageURL + strconv.Itoa(contestNumber)

	client := http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", currentContestPageURL, nil)
	if err != nil {
		return 0, err
	}

	// Get cookie value from context
	cookieVals, ok := ctx.Value(cookieKey).(Cookie)
	if !ok {
		return 0, fmt.Errorf("cookie values not found in context")
	}

	req.AddCookie(&http.Cookie{Name: "REVEL_SESSION", Value: cookieVals.RevelSession})
	req.AddCookie(&http.Cookie{Name: "_ga", Value: cookieVals.Ga})
	req.AddCookie(&http.Cookie{Name: "_ga_RC512FD18N", Value: cookieVals.Ga_})
	req.AddCookie(&http.Cookie{Name: "timeDelta", Value: cookieVals.TimeDelta})
	req.AddCookie(&http.Cookie{Name: "REVEL_FLASH", Value: cookieVals.RevelFlash})

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("unexpected status code %d while fetching contest page %q", resp.StatusCode, currentContestPageURL)
	}

	html, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	taskScoreRegex := regexp.MustCompile(`<td[^>]*>\s*([A-Z])\s*</td>\s*<td[^>]*>\s*(\d+)\s*</td>`)
	matches := taskScoreRegex.FindAllStringSubmatch(string(html), -1)

	if len(matches) == 0 {
		return 0, fmt.Errorf("could not find any task rows in contest page %q", currentContestPageURL)
	}

	uniqueTasks := make(map[string]struct{})
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		task := match[1]
		if len(task) != 1 {
			continue
		}
		if _, exists := uniqueTasks[task]; !exists {
			uniqueTasks[task] = struct{}{}
		}
	}

	if len(uniqueTasks) == 0 {
		return 0, fmt.Errorf("parsed zero unique tasks from contest page %q", currentContestPageURL)
	}

	return len(uniqueTasks), nil
}

func downloadAndCreateTestcases(ctx context.Context, contestNumber int, directoryPath string, alpha string, wg *sync.WaitGroup, errChan chan<- error) {

	defer wg.Done()

	var client http.Client

	// Build url.
	reqUrl := fmt.Sprintf("%s%d/tasks/abc%d_%s",
		getProblemsPageURL,
		contestNumber,
		contestNumber,
		strings.ToLower(alpha),
	)

	fmt.Println(reqUrl)

	req, err := http.NewRequestWithContext(ctx, "GET", reqUrl, nil)
	if err != nil {
		errChan <- err
		return
	}

	cookieVals, ok := ctx.Value(cookieKey).(Cookie)
	if !ok {
		errChan <- fmt.Errorf("unable to get cookie values: %s", reqUrl)
		return
	}

	req.AddCookie(&http.Cookie{Name: "REVEL_SESSION", Value: cookieVals.RevelSession})
	req.AddCookie(&http.Cookie{Name: "_ga", Value: cookieVals.Ga})
	req.AddCookie(&http.Cookie{Name: "_ga_RC512FD18N", Value: cookieVals.Ga_})
	req.AddCookie(&http.Cookie{Name: "timeDelta", Value: cookieVals.TimeDelta})
	req.AddCookie(&http.Cookie{Name: "REVEL_FLASH", Value: cookieVals.RevelFlash})

	resp, err := client.Do(req)
	if err != nil {
		errChan <- err
		return
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			errChan <- err
			return
		}
	}()

	if resp.StatusCode != 200 {
		errChan <- fmt.Errorf("the request resp has this status code: %v", resp.StatusCode)
		return
	}

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		errChan <- err
		return
	}

	testcaseRe := regexp.MustCompile(testcasesRegex)
	matches := testcaseRe.FindAllStringSubmatch(string(htmlBytes), -1)

	directoryPath = filepath.Join(directoryPath, strings.ToUpper(alpha))

	if err := ensureDefaultSource(directoryPath); err != nil {
		errChan <- err
		return
	}

	for idx, m := range matches {

		inputFileName := filepath.Join(directoryPath, fmt.Sprintf("input%s.txt", strconv.Itoa(idx)))
		outputFileName := filepath.Join(directoryPath, fmt.Sprintf("output%s.txt", strconv.Itoa(idx)))

		inputFile, err := os.Create(inputFileName)
		if err != nil {
			errChan <- err
			return
		}

		defer func() {
			if err := inputFile.Close(); err != nil {
				errChan <- err
				return
			}
		}()

		outputFile, err := os.Create(outputFileName)
		if err != nil {
			errChan <- err
			return
		}

		defer func() {
			if err := outputFile.Close(); err != nil {
				errChan <- err
				return
			}
		}()

		_, err = inputFile.WriteString(m[1])
		if err != nil {
			errChan <- err
			return
		}

		_, err = outputFile.WriteString(m[2])
		if err != nil {
			errChan <- err
			return
		}
	}

}

type testCase struct {
	name       string
	inputFile  string
	outputFile string
	input      []byte
	expected   []byte
}

func runTestsInDir(ctx context.Context, dir string) error {
	sourceFile, err := findSourceFile(dir)
	if err != nil {
		return err
	}

	testcases, err := collectTestcases(dir)
	if err != nil {
		return err
	}

	binPath, err := compileSource(ctx, dir, sourceFile)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(binPath)
	}()

	fmt.Printf("Using source: %s (%d testcases)\n", filepath.Base(sourceFile), len(testcases))

	passed := 0
	for idx, tc := range testcases {
		label := tc.name
		if label == "" {
			label = strconv.Itoa(idx)
		}

		ok, testErr := runSingleTest(ctx, binPath, tc)
		if ok {
			fmt.Printf("  [PASS] %s\n", label)
			passed++
			continue
		}

		fmt.Printf("  [FAIL] %s: %v\n", label, testErr)
	}

	fmt.Printf("Result for %s: %d/%d passed\n", dir, passed, len(testcases))
	if passed != len(testcases) {
		return fmt.Errorf("some tests failed in %q", dir)
	}

	return nil
}

func resolveTestTargets(path string) ([]string, error) {
	if path == "" {
		path = "."
	}

	cleanPath := filepath.Clean(path)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", cleanPath)
	}

	hasTests, err := directoryHasTests(cleanPath)
	if err != nil {
		return nil, err
	}

	if hasTests {
		return []string{cleanPath}, nil
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		subDir := filepath.Join(cleanPath, e.Name())
		ok, err := directoryHasTests(subDir)
		if err != nil {
			return nil, err
		}

		if ok {
			dirs = append(dirs, subDir)
		}
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("no testcases found under %q", cleanPath)
	}

	sort.Strings(dirs)
	return dirs, nil
}

func directoryHasTests(dir string) (bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "input*.txt"))
	if err != nil {
		return false, err
	}

	return len(matches) > 0, nil
}

func collectTestcases(dir string) ([]testCase, error) {
	inputFiles, err := filepath.Glob(filepath.Join(dir, "input*.txt"))
	if err != nil {
		return nil, err
	}

	sort.Strings(inputFiles)

	if len(inputFiles) == 0 {
		return nil, fmt.Errorf("no input files found in %q", dir)
	}

	var testcases []testCase

	for _, inputPath := range inputFiles {
		base := filepath.Base(inputPath)
		suffix := strings.TrimPrefix(strings.TrimSuffix(base, ".txt"), "input")
		outputPath := filepath.Join(dir, fmt.Sprintf("output%s.txt", suffix))

		outputBytes, err := os.ReadFile(outputPath)
		if err != nil {
			return nil, fmt.Errorf("expected output file %q: %w", outputPath, err)
		}

		inputBytes, err := os.ReadFile(inputPath)
		if err != nil {
			return nil, err
		}

		testcases = append(testcases, testCase{
			name:       suffix,
			inputFile:  inputPath,
			outputFile: outputPath,
			input:      inputBytes,
			expected:   outputBytes,
		})
	}

	return testcases, nil
}

func findSourceFile(dir string) (string, error) {
	path := filepath.Join(dir, "main.cpp")
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("expected main.cpp in %q: %w", dir, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("expected main.cpp in %q but found a directory", dir)
	}
	return path, nil
}

func ensureDefaultSource(dir string) error {
	target := filepath.Join(dir, "main.cpp")
	_, err := os.Stat(target)
	if err == nil {
		return nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.WriteFile(target, []byte(defaultCPPTemplate), 0644); err != nil {
		return fmt.Errorf("unable to create default main.cpp in %q: %w", dir, err)
	}

	return nil
}

func compileSource(ctx context.Context, workDir string, source string) (string, error) {
	tmpFile, err := os.CreateTemp("", "ac-bin-*")
	if err != nil {
		return "", err
	}
	binPath := tmpFile.Name()

	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	sourceArg := source
	if workDir != "" {
		if absWork, err := filepath.Abs(workDir); err == nil {
			if absSrc, err := filepath.Abs(source); err == nil {
				if rel, err := filepath.Rel(absWork, absSrc); err == nil {
					sourceArg = rel
				}
			}
		}
	}

	cmd := exec.CommandContext(ctx, "g++", "-std=gnu++17", "-O2", "-pipe", "-o", binPath, sourceArg)
	cmd.Dir = workDir

	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to compile %s: %v\n%s", source, err, string(out))
	}

	return binPath, nil
}

func runSingleTest(ctx context.Context, binary string, tc testCase) (bool, error) {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary)
	cmd.Stdin = bytes.NewReader(tc.input)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return false, fmt.Errorf("timeout after 5s")
		}

		trimmedErr := strings.TrimSpace(stderr.String())
		if trimmedErr == "" {
			trimmedErr = err.Error()
		}
		return false, fmt.Errorf("program error: %s", trimmedErr)
	}

	expected := bytes.TrimRight(tc.expected, "\r\n")
	actual := bytes.TrimRight(stdout.Bytes(), "\r\n")

	if !bytes.Equal(expected, actual) {
		return false, fmt.Errorf("mismatch\nexpected:\n%s\nactual:\n%s", string(expected), string(actual))
	}

	return true, nil
}
