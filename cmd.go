package main

import (
	"encoding/json"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
	"log"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var rootCmd = &cobra.Command{
	Use:   strings.ToLower(AppName),
	Short: AppName + " - A Manga Downloader",
	Long:  `The ultimate manga downloader multitool`,
	Run: func(cmd *cobra.Command, args []string) {
		config, _ := cmd.Flags().GetString("config")
		initConfig(config)

		if format, _ := cmd.Flags().GetString("format"); format != "" {
			UserConfig.Format = FormatType(format)
		}

		if err := ValidateConfig(UserConfig); err != nil {
			log.Fatal(err)
		}

		var program *tea.Program

		if UserConfig.UI.Fullscreen {
			program = tea.NewProgram(NewBubble(searchState), tea.WithAltScreen())
		} else {
			program = tea.NewProgram(NewBubble(searchState))
		}

		if err := program.Start(); err != nil {
			log.Fatal(err)
		}
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Long:  fmt.Sprintf("Shows %s versions and build date", AppName),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s version %s\n", accentStyle.Render(strings.ToLower(AppName)), boldStyle.Render(version))
	},
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove cached and temp files",
	Long:  "Removes cached files produced by scraper and temp files from downloader",
	Run: func(cmd *cobra.Command, args []string) {
		counter, bytes := RemoveTemp()
		c, b := RemoveCache()
		counter += c
		bytes += b

		fmt.Printf("%d files removed\nCleaned up %.2fMB\n", counter, BytesToMegabytes(bytes))
	},
}

var cleanupTempCmd = &cobra.Command{
	Use:   "temp",
	Short: "Remove temp files",
	Long:  "Removes temp files produced by downloader",
	Run: func(cmd *cobra.Command, args []string) {
		counter, bytes := RemoveTemp()
		fmt.Printf("%d temp files removed\nCleaned up %.2fMB\n", counter, BytesToMegabytes(bytes))
	},
}

var cleanupCacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Remove cache files",
	Long:  "Removes cache files produced by scraper",
	Run: func(cmd *cobra.Command, args []string) {
		counter, bytes := RemoveCache()
		fmt.Printf("%d cache files removed\nCleaned up %.2fMB\n", counter, BytesToMegabytes(bytes))
	},
}

var inlineCmd = &cobra.Command{
	Use:   "inline",
	Short: "Search & Download manga in inline mode",
	Long: `Search & Download manga in inline mode
Useful for scripting`,
	Run: func(cmd *cobra.Command, args []string) {
		asTemp, _ := cmd.Flags().GetBool("temp")
		defer func() {
			if !asTemp {
				RemoveTemp()
			}
		}()

		config, _ := cmd.Flags().GetString("config")
		initConfig(config)
		if format, _ := cmd.Flags().GetString("format"); format != "" {
			UserConfig.Format = FormatType(format)
		}

		if err := ValidateConfig(UserConfig); err != nil {
			log.Fatal(err)
		}

		var (
			manga []*URL
			wg    sync.WaitGroup
		)

		query, _ := cmd.Flags().GetString("query")
		if query == "" {
			log.Fatal("Query expected")
		}

		wg.Add(len(UserConfig.Scrapers))

		for _, scraper := range UserConfig.Scrapers {
			go func(s *Scraper) {
				defer wg.Done()

				m, err := s.SearchManga(query)

				if err == nil {
					manga = append(manga, m...)
				}
			}(scraper)
		}

		wg.Wait()

		showUrls, _ := cmd.Flags().GetBool("urls")
		asJson, _ := cmd.Flags().GetBool("json")
		if mangaIdx, _ := cmd.Flags().GetInt("manga"); mangaIdx != -1 {
			if mangaIdx > len(manga) || mangaIdx <= 0 {
				log.Fatal("Index out of range")
			}

			selectedManga := manga[mangaIdx-1]

			chapters, err := selectedManga.Scraper.GetChapters(selectedManga)
			if err != nil {
				log.Fatal("Error while getting chapters")
			}

			if chapterIdx, _ := cmd.Flags().GetInt("chapter"); chapterIdx != -1 {

				selectedChapter, ok := Find(chapters, func(c *URL) bool {
					return c.Index == chapterIdx
				})

				if !ok {
					log.Fatal("Index out of range")
				}

				read, _ := cmd.Flags().GetBool("read")

				if read {
					asTemp = true
				}

				chapterPath, err := DownloadChapter(selectedChapter, nil, asTemp)
				if err != nil {
					log.Fatal("Error while downloading chapter")
				}

				if EpubFile != nil {
					EpubFile.SetAuthor(selectedManga.Scraper.Source.Base)
					if err := EpubFile.Write(chapterPath); err != nil {
						log.Fatal("Error while making epub file")
					}
				}

				if read {
					if UserConfig.UseCustomReader {
						_ = open.StartWith(chapterPath, UserConfig.CustomReader)
					} else {
						_ = open.Start(chapterPath)
					}
					return
				}

				fmt.Println(chapterPath)
				return
			}

			if asJson {
				data, err := json.Marshal(chapters)
				if err != nil {
					log.Fatal("Could not get data as json")
				}

				fmt.Println(string(data))
			} else {
				for _, c := range chapters {
					if showUrls {
						fmt.Printf("[%d] %s %s\n", c.Index, c.Info, c.Address)
					} else {
						fmt.Printf("[%d] %s\n", c.Index, c.Info)
					}
				}
			}

		} else {
			if asJson {
				data, err := json.Marshal(manga)
				if err != nil {
					log.Fatal("Could not get data as json")
				}

				fmt.Println(string(data))
			} else {
				for i, m := range manga {
					if showUrls {
						fmt.Printf("[%d] %s %s\n", i+1, m.Info, m.Address)
					} else {
						fmt.Printf("[%d] %s\n", i+1, m.Info)
					}
				}
			}
		}
	},
}

func initConfig(config string) {
	exists, err := Afero.Exists(config)

	if err != nil {
		log.Fatal(errors.New("access to config file denied"))
	}

	if config != "" {
		config = path.Clean(config)
		if !exists {
			log.Fatal(errors.New(fmt.Sprintf("config at path %s doesn't exist", config)))
		}

		UserConfig = GetConfig(config)
	} else {
		UserConfig = GetConfig("") // get config from default config path
	}
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Config actions",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var configWhereCmd = &cobra.Command{
	Use:   "where",
	Short: "Show config location",
	Long:  "Show path where config is located if it exists.\nOtherwise show path where it is expected to be",
	Run: func(cmd *cobra.Command, args []string) {
		configPath, err := GetConfigPath()

		if err != nil {
			log.Fatal("Can't get config location, permission denied, probably")
		}

		exists, err := Afero.Exists(configPath)

		if err != nil {
			log.Fatalf("Can't understand if config exists or not. It is expected at\n%s\n", configPath)
		}

		if exists {
			fmt.Printf("Config exists at\n%s\n", successStyle.Render(configPath))
		} else {
			fmt.Printf("Config doesn't exist, but it is expected to be at\n%s\n", successStyle.Render(configPath))
		}
	},
}

var configPreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview current config",
	Run: func(cmd *cobra.Command, args []string) {
		configPath, err := GetConfigPath()

		if err != nil {
			log.Fatal("Permission to config file was denied")
		}

		exists, err := Afero.Exists(configPath)

		if err != nil {
			log.Fatalf("Permission to config file was denied")
		}

		if !exists {
			log.Fatal("Config doesn't exist")
		}

		contents, err := Afero.ReadFile(configPath)
		if err != nil {
			log.Fatal("Permission to config file was denied")
		}

		fmt.Println(string(contents))
	},
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit config in the default editor",
	Run: func(cmd *cobra.Command, args []string) {
		configPath, err := GetConfigPath()

		if err != nil {
			log.Fatal("Permission to config file was denied")
		}

		err = open.Start(configPath)
		if err != nil {
			log.Fatal("Can't open editor")
		}
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Init default config",
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")

		configPath, err := GetConfigPath()

		if err != nil {
			log.Fatal("Can't get config location, permission denied, probably")
		}

		exists, err := Afero.Exists(configPath)

		var createConfig = func() {
			if err := Afero.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
				log.Fatal("Error while creating file")
			} else if file, err := Afero.Create(configPath); err != nil {
				log.Fatal("Error while creating file")
			} else if _, err = file.Write(DefaultConfigBytes); err != nil {
				log.Fatal("Error while writing to file")
			} else {
				fmt.Printf("Config created at\n%s\n", successStyle.Render(configPath))
			}
		}

		if force {
			createConfig()
			return
		}

		if err != nil {
			log.Fatalf("Can't understand if config exists or not, but it is expected at\n%s\n", configPath)
		}

		if exists {
			log.Fatalf("Config file already exists. Use %s to overwrite it", accentStyle.Render("--force"))
		} else {
			createConfig()
		}
	},
}

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test user config for any errors",
	Run: func(cmd *cobra.Command, args []string) {
		config, _ := cmd.Flags().GetString("config")
		initConfig(config)

		if err := ValidateConfig(UserConfig); err != nil {
			log.Fatal(err)
		} else {
			fmt.Println(successStyle.Render("Everything is OK"))
		}
	},
}

var formatsCmd = &cobra.Command{
	Use:   "formats",
	Short: "Information about available formats",
	Long:  "Show information about available formats with quick description of each",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf(boldStyle.Render("Available formats") + "\n\n")
		for _, format := range AvailableFormats {
			fmt.Printf("%s - %s\n", accentStyle.Render(string(format)), FormatsInfo[format])
		}
	},
}

func CmdExecute() {
	rootCmd.AddCommand(versionCmd)

	cleanupCmd.AddCommand(cleanupTempCmd)
	cleanupCmd.AddCommand(cleanupCacheCmd)
	rootCmd.AddCommand(cleanupCmd)

	configCmd.AddCommand(configWhereCmd)
	configCmd.AddCommand(configPreviewCmd)
	configCmd.AddCommand(configEditCmd)
	configInitCmd.Flags().BoolP("force", "f", false, "overwrite existing config")
	configCmd.AddCommand(configInitCmd)

	configTestCmd.Flags().StringP("config", "c", "", "use config from path")
	configCmd.AddCommand(configTestCmd)
	rootCmd.AddCommand(configCmd)

	inlineCmd.Flags().Int("manga", -1, "choose manga by index")
	inlineCmd.Flags().Int("chapter", -1, "choose and download chapter by index")
	inlineCmd.Flags().StringP("query", "q", "", "manga to search")
	inlineCmd.Flags().BoolP("json", "j", false, "print as json")
	inlineCmd.Flags().StringP("format", "f", "", "use custom format")
	inlineCmd.Flags().BoolP("urls", "u", false, "show urls")
	inlineCmd.Flags().BoolP("temp", "t", false, "download as temp")
	inlineCmd.Flags().BoolP("read", "r", false, "read chapter")
	inlineCmd.Flags().StringP("config", "c", "", "use config from path")
	inlineCmd.Flags().SortFlags = false
	rootCmd.AddCommand(inlineCmd)

	rootCmd.Flags().StringP("config", "c", "", "use config from path")
	rootCmd.Flags().StringP("format", "f", "", "use custom format")

	rootCmd.AddCommand(formatsCmd)

	_ = rootCmd.Execute()
}
