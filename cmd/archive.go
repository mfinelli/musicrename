/*
Copyright © 2020 Mario Finelli

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/

package cmd

import (
	"fmt"
	"github.com/AlecAivazis/survey/v2"
	"github.com/kurin/blazer/b2"
	"github.com/mfinelli/musicrename/crypt"
	"github.com/mfinelli/musicrename/uploader"
	"github.com/mfinelli/musicrename/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

var (
	artist string
	album  string
	year   string
)

// archiveCmd represents the archive command
var archiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Uploads raws purchase archives to the purchase bucket",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// str,e := uploader.FetchShaSumFile(viper.GetString("purchases.bucket"), "t/test/[2000]test/test.txt")
		fp2, e := os.Create("test2.txt")
		if e != nil {
			fmt.Println(e)
			os.Exit(1)
		}
		defer fp2.Close()

		e = uploader.Download(viper.GetString("purchases.bucket"), "t/test/[2000]test/nope.txt", fp2.Name())

		if b2.IsNotExist(e) {
			fmt.Println("not exist!")
			os.Exit(0)
		}
		if e != nil {
			fmt.Println(e)
			os.Exit(1)
		}

		// fmt.Println(str)
		os.Exit(0)


		if !util.VerifyConfig() {
			fmt.Println("missing configuration; run `mr configure`")
			os.Exit(1)
		}

		if artist == "" {
			artistPrompt := &survey.Input{
			Message: "Artist",
		}

		survey.AskOne(artistPrompt, &artist,
			survey.WithValidator(survey.Required))
		}

		if album == "" {
			albumPrompt := &survey.Input{
			Message: "Album",
		}

		survey.AskOne(albumPrompt, &album,
			survey.WithValidator(survey.Required))
		}

		if year == "" {
			yearPrompt := &survey.Input{
			Message: "Album year",
		}

		survey.AskOne(yearPrompt, &year,
			survey.WithValidator(survey.Required))
		}

		start := time.Now()

		sha1, err := util.FileSha1(args[0])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		fmt.Println(sha1)

		encryptStart := time.Now()
		input, er := os.Open(args[0])
		if er != nil {
			fmt.Println(er)
			os.Exit(1)
		}
		defer input.Close()
		tmp, er := ioutil.TempFile(os.TempDir(), "")
		if er != nil {
			fmt.Println(er)
			os.Exit(1)
		}
		defer os.Remove(tmp.Name())
		er = crypt.EncryptFile(input, tmp)
		if er != nil {
			fmt.Println(er)
			os.Exit(1)
		}
		tmp.Close()

		encryptEnd := time.Now()
		fmt.Printf("encrypted %s in %v\n", filepath.Base(args[0]), encryptEnd.Sub(encryptStart))

		// sumStart := time.Now()
		// sha1, err := util.FileSha1(tmp.Name())
		// if err != nil {
		// 	fmt.Println(err)
		// 	os.Exit(1)
		// }
		// sumEnd := time.Now()
		// fmt.Printf("computed sha1 sum in %v\n", sumEnd.Sub(sumStart))

		// tmp2, er := os.Open(tmp.Name())
		// if er != nil {
		// 	fmt.Println(er)
		// 	os.Exit(1)
		// }
		// defer tmp2.Close()
		// output, er := os.Create("output.txt")
		// if er != nil {
		// 	fmt.Println(er)
		// 	os.Exit(1)
		// }
		// defer output.Close()
		// er = crypt.DecryptFile(tmp2, output)
		// if er != nil {
		// 	fmt.Println(er)
		// 	os.Exit(1)
		// }

		uploadStart := time.Now()

		// os.Exit(0)
		// fmt.Println(util.PrefixFromArtistAlbum(artist, year, album))
		key := fmt.Sprintf("%s/%s", util.PrefixFromArtistAlbum(artist, year, album), filepath.Base(args[0]))
		err = uploader.Upload2(viper.GetString("purchases.bucket"), key, tmp.Name())
		// err := uploader.Upload(viper.GetString("purchases.bucket"), key, tmp.Name())

		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		uploadEnd := time.Now()
		fmt.Printf("uploaded %s in %v\n", filepath.Base(args[0]), uploadEnd.Sub(uploadStart))

		end := time.Now()
		fmt.Printf("finished in %v\n", end.Sub(start))
	},
}

func init() {
	rootCmd.AddCommand(archiveCmd)

	archiveCmd.Flags().StringVarP(&artist, "artist", "A", "", "artist name")
	archiveCmd.Flags().StringVarP(&album, "album", "a", "", "album name")
	archiveCmd.Flags().StringVarP(&year, "year", "y", "", "album year")

	// archiveCmd.MarkFlagRequired("artist")
	// archiveCmd.MarkFlagRequired("album")
	// archiveCmd.MarkFlagRequired("year")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// archiveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// archiveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
