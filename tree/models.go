/*
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package tree

type Artist struct {
	Name     string
	Path     string
	RealPath string
	Albums   []Album
}

type Album struct {
	Artist   *Artist
	Year     string
	Name     string
	Path     string
	RealPath string
	Songs    []Song
	Cover    Artwork
	Artworks []Artwork
	Extras   []Extra
	Unknown  []Unkown
}

type Song struct {
	Album    *Album
	Disc     int
	Track    int
	Name     string
	Path     string
	RealPath string
	Type     string
}

type Artwork struct {
	Album    *Album
	Path     string
	RealPath string
	Type     string
}

type Extra struct {
	Path     string
	RealPath string
	Type     string
}

type Unkown struct {
	Path     string
	RealPath string
}
