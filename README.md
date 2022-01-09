## pricer
Command line utility that takes a BOM and estimates the cost

## Overview
Have you ever looked at some cool maker / DIY project that provided a list of links to components needed to build the project and wondered what the total cost would be? The author of the blog, webpage, YouTube video, etc. usually doesn't list the prices because they're subject to change, which means you have to click on each of the links and add it all up. No big deal if it's a few links but what if it is, for example, a CNC router with dozens or hundreds of parts? The goal of `pricer` is to take an input file (only `.tsv` is supported currently) and give a reasonable estimate.

## Limitations
* `pricer` requires Chrome browser to be installed. It doesn't work with Firefox, Safari, etc.
  * Why does it even need a browser? Some vendor sites set the price elements in the HTML dynamically after the page has loaded. Using Chrome's web driver is an easy workaround.
  * Web driver?. What are you even talking about? `pricer` starts up several headless (hidden) Chrome web browsers in the background and uses them to visit each of the sites to collect the prices, just as if you were doing it manually in Chrome.
* Currently, the biggest limitation is that `pricer` only knows how to scrape component prices from https://www.banggood.com. It still needs to be taught a lot more (Amazon, AliExpress, Adafruit, etc.) to be useful.
* Sometimes there's no way to link directly to a specific component. E.g., the website requires a size and/or color to be selected before displaying a price. Could that be automated in the future? Maybe.
* It's not uncommon for someone to list the total number of something (e.g., screws) and then link to a bulk box that satisfies the full quantity. Currently, `pricer` can't figure that out on its own. You'll have to update the quantity in the input file.

## Install
It has to be built from source. You'll need to have the [Go tools](https://go.dev/learn) installed. Then run:

```
go get https://github.com/dgnorton/pricer
```

## Usage
Create a `.tsv` (tab separated values) file with the items you want to look up.

`data.tsv`
```
Name    Quantity        Banggood        Aliexpress
Profile 20×40 600 mm    2       https://bit.ly/3iGGfdM  https://bit.ly/30VoyRv
Profile 20×40 666 mm    2       https://bit.ly/2XYn3QE  https://bit.ly/3kC0ZoJ
Profile 20×80 600 mm    2       https://bit.ly/2Y03gQL  https://bit.ly/2PRw7Cs
Trapezoidal Lead Screw 220 mm   1       https://bit.ly/2PS5r4C  https://bit.ly/31Nef0Z
```

Note: there can be multiple vendor columns. The example above shoes columns for Banggood and Aliexpress, although `pricer` only knows how to read Banggood so far.

Then run:

```
pricer data.tsv
```

The output may look something like:

```
error: https://bit.ly/2Y03gQL: couldn't find <span class="main-price"
Description          Quantity Vendor   Price  Shipping URL
Profile 20×40 600 mm 2        Banggood 19.00  5.00     https://bit.ly/3iGGfdM
Profile 20×40 666 mm 2        Banggood 30.00  0.00     https://bit.ly/2XYn3QE
Profile 20×80 600 mm 2        Banggood ?.??   ?.??     https://bit.ly/2Y03gQL
Total                                  101.00 10.00     
Grand Total                            112.00
```

It's likely it will encounter errors with some item pages. There's no common or clean format for product pages, even within a single vendor. `pricer` does the best it can and tries to warn you about the rest. Also note the `?.??` price for the bit.ly link that had the error. Look on the bright side, now you only have to look up a few links!
