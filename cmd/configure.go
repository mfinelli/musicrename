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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
)

// configureCmd represents the configure command
var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "(Re-)configures musicrename",
	Run: func(cmd *cobra.Command, args []string) {
		accessKey := ""
		secretKey := ""
		purchasesRegion := ""
		purchasesBucket := ""

		accessPrompt := &survey.Input{
			Message: "B2 Access Key",
		}

		survey.AskOne(accessPrompt, &accessKey,
			survey.WithValidator(survey.Required))

		secretPrompt := &survey.Password{
			Message: "B2 Secret Key",
		}

		survey.AskOne(secretPrompt, &secretKey,
			survey.WithValidator(survey.Required))

		purchasesRegionPrompt := &survey.Input{
			Message: "B2 Purchase Bucket Region",
			Help:    "e.g., us-west-001",
		}

		survey.AskOne(purchasesRegionPrompt, &purchasesRegion,
			survey.WithValidator(survey.Required))

		purchasesBucketPrompt := &survey.Input{
			Message: "B2 Purchases Bucket Name",
		}

		survey.AskOne(purchasesBucketPrompt, &purchasesBucket,
			survey.WithValidator(survey.Required))

		viper.Set("accesskey", accessKey)
		viper.Set("secretkey", secretKey)
		viper.Set("purchases.bucket", purchasesBucket)
		viper.Set("purchases.region", purchasesRegion)

		// https://github.com/spf13/viper/issues/851#issuecomment-631392387
		if err := viper.SafeWriteConfig(); err != nil {
			if err = viper.WriteConfig(); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(configureCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// configureCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// configureCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
