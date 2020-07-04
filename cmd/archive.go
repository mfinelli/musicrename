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
	"github.com/mfinelli/musicrename/crypt"
	"github.com/mfinelli/musicrename/uploader"
	"github.com/mfinelli/musicrename/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io/ioutil"
	"os"
	"path/filepath"
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

		// os.Exit(0)
		// fmt.Println(util.PrefixFromArtistAlbum(artist, year, album))
		key := fmt.Sprintf("%s/%s", util.PrefixFromArtistAlbum(artist, year, album), filepath.Base(args[0]))
		err := uploader.Upload(viper.GetString("purchases.bucket"), key, tmp.Name())

		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(archiveCmd)

	archiveCmd.Flags().StringVarP(&artist, "artist", "A", "", "artist name")
	archiveCmd.Flags().StringVarP(&album, "album", "a", "", "album name")
	archiveCmd.Flags().StringVarP(&year, "year", "y", "", "album year")

	archiveCmd.MarkFlagRequired("artist")
	archiveCmd.MarkFlagRequired("album")
	archiveCmd.MarkFlagRequired("year")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// archiveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// archiveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
