//go:build kernel_load

// Code generated for load_test.go; DO NOT EDIT. Each [N]byte is a distinct
// resource type, so the load test can declare loadComponents resources.

package kernel

// initComponents initializes every load component as a resource.
var initComponents = [...]func(c *Configurator){
	func(c *Configurator) { c.InitResource([1]byte{}) },
	func(c *Configurator) { c.InitResource([2]byte{}) },
	func(c *Configurator) { c.InitResource([3]byte{}) },
	func(c *Configurator) { c.InitResource([4]byte{}) },
	func(c *Configurator) { c.InitResource([5]byte{}) },
	func(c *Configurator) { c.InitResource([6]byte{}) },
	func(c *Configurator) { c.InitResource([7]byte{}) },
	func(c *Configurator) { c.InitResource([8]byte{}) },
	func(c *Configurator) { c.InitResource([9]byte{}) },
	func(c *Configurator) { c.InitResource([10]byte{}) },
	func(c *Configurator) { c.InitResource([11]byte{}) },
	func(c *Configurator) { c.InitResource([12]byte{}) },
	func(c *Configurator) { c.InitResource([13]byte{}) },
	func(c *Configurator) { c.InitResource([14]byte{}) },
	func(c *Configurator) { c.InitResource([15]byte{}) },
	func(c *Configurator) { c.InitResource([16]byte{}) },
	func(c *Configurator) { c.InitResource([17]byte{}) },
	func(c *Configurator) { c.InitResource([18]byte{}) },
	func(c *Configurator) { c.InitResource([19]byte{}) },
	func(c *Configurator) { c.InitResource([20]byte{}) },
	func(c *Configurator) { c.InitResource([21]byte{}) },
	func(c *Configurator) { c.InitResource([22]byte{}) },
	func(c *Configurator) { c.InitResource([23]byte{}) },
	func(c *Configurator) { c.InitResource([24]byte{}) },
	func(c *Configurator) { c.InitResource([25]byte{}) },
	func(c *Configurator) { c.InitResource([26]byte{}) },
	func(c *Configurator) { c.InitResource([27]byte{}) },
	func(c *Configurator) { c.InitResource([28]byte{}) },
	func(c *Configurator) { c.InitResource([29]byte{}) },
	func(c *Configurator) { c.InitResource([30]byte{}) },
	func(c *Configurator) { c.InitResource([31]byte{}) },
	func(c *Configurator) { c.InitResource([32]byte{}) },
	func(c *Configurator) { c.InitResource([33]byte{}) },
	func(c *Configurator) { c.InitResource([34]byte{}) },
	func(c *Configurator) { c.InitResource([35]byte{}) },
	func(c *Configurator) { c.InitResource([36]byte{}) },
	func(c *Configurator) { c.InitResource([37]byte{}) },
	func(c *Configurator) { c.InitResource([38]byte{}) },
	func(c *Configurator) { c.InitResource([39]byte{}) },
	func(c *Configurator) { c.InitResource([40]byte{}) },
	func(c *Configurator) { c.InitResource([41]byte{}) },
	func(c *Configurator) { c.InitResource([42]byte{}) },
	func(c *Configurator) { c.InitResource([43]byte{}) },
	func(c *Configurator) { c.InitResource([44]byte{}) },
	func(c *Configurator) { c.InitResource([45]byte{}) },
	func(c *Configurator) { c.InitResource([46]byte{}) },
	func(c *Configurator) { c.InitResource([47]byte{}) },
	func(c *Configurator) { c.InitResource([48]byte{}) },
	func(c *Configurator) { c.InitResource([49]byte{}) },
	func(c *Configurator) { c.InitResource([50]byte{}) },
	func(c *Configurator) { c.InitResource([51]byte{}) },
	func(c *Configurator) { c.InitResource([52]byte{}) },
	func(c *Configurator) { c.InitResource([53]byte{}) },
	func(c *Configurator) { c.InitResource([54]byte{}) },
	func(c *Configurator) { c.InitResource([55]byte{}) },
	func(c *Configurator) { c.InitResource([56]byte{}) },
	func(c *Configurator) { c.InitResource([57]byte{}) },
	func(c *Configurator) { c.InitResource([58]byte{}) },
	func(c *Configurator) { c.InitResource([59]byte{}) },
	func(c *Configurator) { c.InitResource([60]byte{}) },
	func(c *Configurator) { c.InitResource([61]byte{}) },
	func(c *Configurator) { c.InitResource([62]byte{}) },
	func(c *Configurator) { c.InitResource([63]byte{}) },
	func(c *Configurator) { c.InitResource([64]byte{}) },
	func(c *Configurator) { c.InitResource([65]byte{}) },
	func(c *Configurator) { c.InitResource([66]byte{}) },
	func(c *Configurator) { c.InitResource([67]byte{}) },
	func(c *Configurator) { c.InitResource([68]byte{}) },
	func(c *Configurator) { c.InitResource([69]byte{}) },
	func(c *Configurator) { c.InitResource([70]byte{}) },
	func(c *Configurator) { c.InitResource([71]byte{}) },
	func(c *Configurator) { c.InitResource([72]byte{}) },
	func(c *Configurator) { c.InitResource([73]byte{}) },
	func(c *Configurator) { c.InitResource([74]byte{}) },
	func(c *Configurator) { c.InitResource([75]byte{}) },
	func(c *Configurator) { c.InitResource([76]byte{}) },
	func(c *Configurator) { c.InitResource([77]byte{}) },
	func(c *Configurator) { c.InitResource([78]byte{}) },
	func(c *Configurator) { c.InitResource([79]byte{}) },
	func(c *Configurator) { c.InitResource([80]byte{}) },
	func(c *Configurator) { c.InitResource([81]byte{}) },
	func(c *Configurator) { c.InitResource([82]byte{}) },
	func(c *Configurator) { c.InitResource([83]byte{}) },
	func(c *Configurator) { c.InitResource([84]byte{}) },
	func(c *Configurator) { c.InitResource([85]byte{}) },
	func(c *Configurator) { c.InitResource([86]byte{}) },
	func(c *Configurator) { c.InitResource([87]byte{}) },
	func(c *Configurator) { c.InitResource([88]byte{}) },
	func(c *Configurator) { c.InitResource([89]byte{}) },
	func(c *Configurator) { c.InitResource([90]byte{}) },
	func(c *Configurator) { c.InitResource([91]byte{}) },
	func(c *Configurator) { c.InitResource([92]byte{}) },
	func(c *Configurator) { c.InitResource([93]byte{}) },
	func(c *Configurator) { c.InitResource([94]byte{}) },
	func(c *Configurator) { c.InitResource([95]byte{}) },
	func(c *Configurator) { c.InitResource([96]byte{}) },
	func(c *Configurator) { c.InitResource([97]byte{}) },
	func(c *Configurator) { c.InitResource([98]byte{}) },
	func(c *Configurator) { c.InitResource([99]byte{}) },
	func(c *Configurator) { c.InitResource([100]byte{}) },
	func(c *Configurator) { c.InitResource([101]byte{}) },
	func(c *Configurator) { c.InitResource([102]byte{}) },
	func(c *Configurator) { c.InitResource([103]byte{}) },
	func(c *Configurator) { c.InitResource([104]byte{}) },
	func(c *Configurator) { c.InitResource([105]byte{}) },
	func(c *Configurator) { c.InitResource([106]byte{}) },
	func(c *Configurator) { c.InitResource([107]byte{}) },
	func(c *Configurator) { c.InitResource([108]byte{}) },
	func(c *Configurator) { c.InitResource([109]byte{}) },
	func(c *Configurator) { c.InitResource([110]byte{}) },
	func(c *Configurator) { c.InitResource([111]byte{}) },
	func(c *Configurator) { c.InitResource([112]byte{}) },
	func(c *Configurator) { c.InitResource([113]byte{}) },
	func(c *Configurator) { c.InitResource([114]byte{}) },
	func(c *Configurator) { c.InitResource([115]byte{}) },
	func(c *Configurator) { c.InitResource([116]byte{}) },
	func(c *Configurator) { c.InitResource([117]byte{}) },
	func(c *Configurator) { c.InitResource([118]byte{}) },
	func(c *Configurator) { c.InitResource([119]byte{}) },
	func(c *Configurator) { c.InitResource([120]byte{}) },
	func(c *Configurator) { c.InitResource([121]byte{}) },
	func(c *Configurator) { c.InitResource([122]byte{}) },
	func(c *Configurator) { c.InitResource([123]byte{}) },
	func(c *Configurator) { c.InitResource([124]byte{}) },
	func(c *Configurator) { c.InitResource([125]byte{}) },
	func(c *Configurator) { c.InitResource([126]byte{}) },
	func(c *Configurator) { c.InitResource([127]byte{}) },
	func(c *Configurator) { c.InitResource([128]byte{}) },
	func(c *Configurator) { c.InitResource([129]byte{}) },
	func(c *Configurator) { c.InitResource([130]byte{}) },
	func(c *Configurator) { c.InitResource([131]byte{}) },
	func(c *Configurator) { c.InitResource([132]byte{}) },
	func(c *Configurator) { c.InitResource([133]byte{}) },
	func(c *Configurator) { c.InitResource([134]byte{}) },
	func(c *Configurator) { c.InitResource([135]byte{}) },
	func(c *Configurator) { c.InitResource([136]byte{}) },
	func(c *Configurator) { c.InitResource([137]byte{}) },
	func(c *Configurator) { c.InitResource([138]byte{}) },
	func(c *Configurator) { c.InitResource([139]byte{}) },
	func(c *Configurator) { c.InitResource([140]byte{}) },
	func(c *Configurator) { c.InitResource([141]byte{}) },
	func(c *Configurator) { c.InitResource([142]byte{}) },
	func(c *Configurator) { c.InitResource([143]byte{}) },
	func(c *Configurator) { c.InitResource([144]byte{}) },
	func(c *Configurator) { c.InitResource([145]byte{}) },
	func(c *Configurator) { c.InitResource([146]byte{}) },
	func(c *Configurator) { c.InitResource([147]byte{}) },
	func(c *Configurator) { c.InitResource([148]byte{}) },
	func(c *Configurator) { c.InitResource([149]byte{}) },
	func(c *Configurator) { c.InitResource([150]byte{}) },
	func(c *Configurator) { c.InitResource([151]byte{}) },
	func(c *Configurator) { c.InitResource([152]byte{}) },
	func(c *Configurator) { c.InitResource([153]byte{}) },
	func(c *Configurator) { c.InitResource([154]byte{}) },
	func(c *Configurator) { c.InitResource([155]byte{}) },
	func(c *Configurator) { c.InitResource([156]byte{}) },
	func(c *Configurator) { c.InitResource([157]byte{}) },
	func(c *Configurator) { c.InitResource([158]byte{}) },
	func(c *Configurator) { c.InitResource([159]byte{}) },
	func(c *Configurator) { c.InitResource([160]byte{}) },
	func(c *Configurator) { c.InitResource([161]byte{}) },
	func(c *Configurator) { c.InitResource([162]byte{}) },
	func(c *Configurator) { c.InitResource([163]byte{}) },
	func(c *Configurator) { c.InitResource([164]byte{}) },
	func(c *Configurator) { c.InitResource([165]byte{}) },
	func(c *Configurator) { c.InitResource([166]byte{}) },
	func(c *Configurator) { c.InitResource([167]byte{}) },
	func(c *Configurator) { c.InitResource([168]byte{}) },
	func(c *Configurator) { c.InitResource([169]byte{}) },
	func(c *Configurator) { c.InitResource([170]byte{}) },
	func(c *Configurator) { c.InitResource([171]byte{}) },
	func(c *Configurator) { c.InitResource([172]byte{}) },
	func(c *Configurator) { c.InitResource([173]byte{}) },
	func(c *Configurator) { c.InitResource([174]byte{}) },
	func(c *Configurator) { c.InitResource([175]byte{}) },
	func(c *Configurator) { c.InitResource([176]byte{}) },
	func(c *Configurator) { c.InitResource([177]byte{}) },
	func(c *Configurator) { c.InitResource([178]byte{}) },
	func(c *Configurator) { c.InitResource([179]byte{}) },
	func(c *Configurator) { c.InitResource([180]byte{}) },
	func(c *Configurator) { c.InitResource([181]byte{}) },
	func(c *Configurator) { c.InitResource([182]byte{}) },
	func(c *Configurator) { c.InitResource([183]byte{}) },
	func(c *Configurator) { c.InitResource([184]byte{}) },
	func(c *Configurator) { c.InitResource([185]byte{}) },
	func(c *Configurator) { c.InitResource([186]byte{}) },
	func(c *Configurator) { c.InitResource([187]byte{}) },
	func(c *Configurator) { c.InitResource([188]byte{}) },
	func(c *Configurator) { c.InitResource([189]byte{}) },
	func(c *Configurator) { c.InitResource([190]byte{}) },
	func(c *Configurator) { c.InitResource([191]byte{}) },
	func(c *Configurator) { c.InitResource([192]byte{}) },
	func(c *Configurator) { c.InitResource([193]byte{}) },
	func(c *Configurator) { c.InitResource([194]byte{}) },
	func(c *Configurator) { c.InitResource([195]byte{}) },
	func(c *Configurator) { c.InitResource([196]byte{}) },
	func(c *Configurator) { c.InitResource([197]byte{}) },
	func(c *Configurator) { c.InitResource([198]byte{}) },
	func(c *Configurator) { c.InitResource([199]byte{}) },
	func(c *Configurator) { c.InitResource([200]byte{}) },
}

// lockComponents declares a read or write lock on each load component.
var lockComponents = [...]func(access *Access, write bool){
	func(a *Access, write bool) {
		if write {
			a.Write[[1]byte]()
		} else {
			a.Read[[1]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[2]byte]()
		} else {
			a.Read[[2]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[3]byte]()
		} else {
			a.Read[[3]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[4]byte]()
		} else {
			a.Read[[4]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[5]byte]()
		} else {
			a.Read[[5]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[6]byte]()
		} else {
			a.Read[[6]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[7]byte]()
		} else {
			a.Read[[7]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[8]byte]()
		} else {
			a.Read[[8]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[9]byte]()
		} else {
			a.Read[[9]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[10]byte]()
		} else {
			a.Read[[10]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[11]byte]()
		} else {
			a.Read[[11]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[12]byte]()
		} else {
			a.Read[[12]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[13]byte]()
		} else {
			a.Read[[13]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[14]byte]()
		} else {
			a.Read[[14]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[15]byte]()
		} else {
			a.Read[[15]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[16]byte]()
		} else {
			a.Read[[16]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[17]byte]()
		} else {
			a.Read[[17]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[18]byte]()
		} else {
			a.Read[[18]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[19]byte]()
		} else {
			a.Read[[19]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[20]byte]()
		} else {
			a.Read[[20]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[21]byte]()
		} else {
			a.Read[[21]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[22]byte]()
		} else {
			a.Read[[22]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[23]byte]()
		} else {
			a.Read[[23]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[24]byte]()
		} else {
			a.Read[[24]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[25]byte]()
		} else {
			a.Read[[25]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[26]byte]()
		} else {
			a.Read[[26]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[27]byte]()
		} else {
			a.Read[[27]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[28]byte]()
		} else {
			a.Read[[28]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[29]byte]()
		} else {
			a.Read[[29]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[30]byte]()
		} else {
			a.Read[[30]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[31]byte]()
		} else {
			a.Read[[31]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[32]byte]()
		} else {
			a.Read[[32]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[33]byte]()
		} else {
			a.Read[[33]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[34]byte]()
		} else {
			a.Read[[34]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[35]byte]()
		} else {
			a.Read[[35]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[36]byte]()
		} else {
			a.Read[[36]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[37]byte]()
		} else {
			a.Read[[37]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[38]byte]()
		} else {
			a.Read[[38]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[39]byte]()
		} else {
			a.Read[[39]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[40]byte]()
		} else {
			a.Read[[40]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[41]byte]()
		} else {
			a.Read[[41]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[42]byte]()
		} else {
			a.Read[[42]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[43]byte]()
		} else {
			a.Read[[43]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[44]byte]()
		} else {
			a.Read[[44]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[45]byte]()
		} else {
			a.Read[[45]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[46]byte]()
		} else {
			a.Read[[46]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[47]byte]()
		} else {
			a.Read[[47]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[48]byte]()
		} else {
			a.Read[[48]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[49]byte]()
		} else {
			a.Read[[49]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[50]byte]()
		} else {
			a.Read[[50]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[51]byte]()
		} else {
			a.Read[[51]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[52]byte]()
		} else {
			a.Read[[52]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[53]byte]()
		} else {
			a.Read[[53]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[54]byte]()
		} else {
			a.Read[[54]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[55]byte]()
		} else {
			a.Read[[55]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[56]byte]()
		} else {
			a.Read[[56]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[57]byte]()
		} else {
			a.Read[[57]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[58]byte]()
		} else {
			a.Read[[58]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[59]byte]()
		} else {
			a.Read[[59]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[60]byte]()
		} else {
			a.Read[[60]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[61]byte]()
		} else {
			a.Read[[61]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[62]byte]()
		} else {
			a.Read[[62]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[63]byte]()
		} else {
			a.Read[[63]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[64]byte]()
		} else {
			a.Read[[64]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[65]byte]()
		} else {
			a.Read[[65]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[66]byte]()
		} else {
			a.Read[[66]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[67]byte]()
		} else {
			a.Read[[67]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[68]byte]()
		} else {
			a.Read[[68]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[69]byte]()
		} else {
			a.Read[[69]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[70]byte]()
		} else {
			a.Read[[70]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[71]byte]()
		} else {
			a.Read[[71]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[72]byte]()
		} else {
			a.Read[[72]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[73]byte]()
		} else {
			a.Read[[73]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[74]byte]()
		} else {
			a.Read[[74]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[75]byte]()
		} else {
			a.Read[[75]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[76]byte]()
		} else {
			a.Read[[76]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[77]byte]()
		} else {
			a.Read[[77]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[78]byte]()
		} else {
			a.Read[[78]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[79]byte]()
		} else {
			a.Read[[79]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[80]byte]()
		} else {
			a.Read[[80]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[81]byte]()
		} else {
			a.Read[[81]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[82]byte]()
		} else {
			a.Read[[82]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[83]byte]()
		} else {
			a.Read[[83]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[84]byte]()
		} else {
			a.Read[[84]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[85]byte]()
		} else {
			a.Read[[85]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[86]byte]()
		} else {
			a.Read[[86]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[87]byte]()
		} else {
			a.Read[[87]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[88]byte]()
		} else {
			a.Read[[88]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[89]byte]()
		} else {
			a.Read[[89]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[90]byte]()
		} else {
			a.Read[[90]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[91]byte]()
		} else {
			a.Read[[91]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[92]byte]()
		} else {
			a.Read[[92]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[93]byte]()
		} else {
			a.Read[[93]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[94]byte]()
		} else {
			a.Read[[94]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[95]byte]()
		} else {
			a.Read[[95]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[96]byte]()
		} else {
			a.Read[[96]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[97]byte]()
		} else {
			a.Read[[97]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[98]byte]()
		} else {
			a.Read[[98]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[99]byte]()
		} else {
			a.Read[[99]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[100]byte]()
		} else {
			a.Read[[100]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[101]byte]()
		} else {
			a.Read[[101]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[102]byte]()
		} else {
			a.Read[[102]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[103]byte]()
		} else {
			a.Read[[103]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[104]byte]()
		} else {
			a.Read[[104]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[105]byte]()
		} else {
			a.Read[[105]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[106]byte]()
		} else {
			a.Read[[106]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[107]byte]()
		} else {
			a.Read[[107]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[108]byte]()
		} else {
			a.Read[[108]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[109]byte]()
		} else {
			a.Read[[109]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[110]byte]()
		} else {
			a.Read[[110]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[111]byte]()
		} else {
			a.Read[[111]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[112]byte]()
		} else {
			a.Read[[112]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[113]byte]()
		} else {
			a.Read[[113]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[114]byte]()
		} else {
			a.Read[[114]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[115]byte]()
		} else {
			a.Read[[115]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[116]byte]()
		} else {
			a.Read[[116]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[117]byte]()
		} else {
			a.Read[[117]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[118]byte]()
		} else {
			a.Read[[118]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[119]byte]()
		} else {
			a.Read[[119]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[120]byte]()
		} else {
			a.Read[[120]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[121]byte]()
		} else {
			a.Read[[121]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[122]byte]()
		} else {
			a.Read[[122]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[123]byte]()
		} else {
			a.Read[[123]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[124]byte]()
		} else {
			a.Read[[124]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[125]byte]()
		} else {
			a.Read[[125]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[126]byte]()
		} else {
			a.Read[[126]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[127]byte]()
		} else {
			a.Read[[127]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[128]byte]()
		} else {
			a.Read[[128]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[129]byte]()
		} else {
			a.Read[[129]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[130]byte]()
		} else {
			a.Read[[130]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[131]byte]()
		} else {
			a.Read[[131]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[132]byte]()
		} else {
			a.Read[[132]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[133]byte]()
		} else {
			a.Read[[133]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[134]byte]()
		} else {
			a.Read[[134]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[135]byte]()
		} else {
			a.Read[[135]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[136]byte]()
		} else {
			a.Read[[136]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[137]byte]()
		} else {
			a.Read[[137]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[138]byte]()
		} else {
			a.Read[[138]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[139]byte]()
		} else {
			a.Read[[139]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[140]byte]()
		} else {
			a.Read[[140]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[141]byte]()
		} else {
			a.Read[[141]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[142]byte]()
		} else {
			a.Read[[142]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[143]byte]()
		} else {
			a.Read[[143]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[144]byte]()
		} else {
			a.Read[[144]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[145]byte]()
		} else {
			a.Read[[145]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[146]byte]()
		} else {
			a.Read[[146]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[147]byte]()
		} else {
			a.Read[[147]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[148]byte]()
		} else {
			a.Read[[148]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[149]byte]()
		} else {
			a.Read[[149]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[150]byte]()
		} else {
			a.Read[[150]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[151]byte]()
		} else {
			a.Read[[151]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[152]byte]()
		} else {
			a.Read[[152]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[153]byte]()
		} else {
			a.Read[[153]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[154]byte]()
		} else {
			a.Read[[154]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[155]byte]()
		} else {
			a.Read[[155]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[156]byte]()
		} else {
			a.Read[[156]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[157]byte]()
		} else {
			a.Read[[157]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[158]byte]()
		} else {
			a.Read[[158]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[159]byte]()
		} else {
			a.Read[[159]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[160]byte]()
		} else {
			a.Read[[160]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[161]byte]()
		} else {
			a.Read[[161]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[162]byte]()
		} else {
			a.Read[[162]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[163]byte]()
		} else {
			a.Read[[163]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[164]byte]()
		} else {
			a.Read[[164]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[165]byte]()
		} else {
			a.Read[[165]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[166]byte]()
		} else {
			a.Read[[166]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[167]byte]()
		} else {
			a.Read[[167]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[168]byte]()
		} else {
			a.Read[[168]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[169]byte]()
		} else {
			a.Read[[169]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[170]byte]()
		} else {
			a.Read[[170]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[171]byte]()
		} else {
			a.Read[[171]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[172]byte]()
		} else {
			a.Read[[172]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[173]byte]()
		} else {
			a.Read[[173]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[174]byte]()
		} else {
			a.Read[[174]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[175]byte]()
		} else {
			a.Read[[175]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[176]byte]()
		} else {
			a.Read[[176]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[177]byte]()
		} else {
			a.Read[[177]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[178]byte]()
		} else {
			a.Read[[178]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[179]byte]()
		} else {
			a.Read[[179]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[180]byte]()
		} else {
			a.Read[[180]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[181]byte]()
		} else {
			a.Read[[181]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[182]byte]()
		} else {
			a.Read[[182]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[183]byte]()
		} else {
			a.Read[[183]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[184]byte]()
		} else {
			a.Read[[184]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[185]byte]()
		} else {
			a.Read[[185]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[186]byte]()
		} else {
			a.Read[[186]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[187]byte]()
		} else {
			a.Read[[187]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[188]byte]()
		} else {
			a.Read[[188]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[189]byte]()
		} else {
			a.Read[[189]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[190]byte]()
		} else {
			a.Read[[190]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[191]byte]()
		} else {
			a.Read[[191]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[192]byte]()
		} else {
			a.Read[[192]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[193]byte]()
		} else {
			a.Read[[193]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[194]byte]()
		} else {
			a.Read[[194]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[195]byte]()
		} else {
			a.Read[[195]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[196]byte]()
		} else {
			a.Read[[196]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[197]byte]()
		} else {
			a.Read[[197]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[198]byte]()
		} else {
			a.Read[[198]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[199]byte]()
		} else {
			a.Read[[199]byte]()
		}
	},
	func(a *Access, write bool) {
		if write {
			a.Write[[200]byte]()
		} else {
			a.Read[[200]byte]()
		}
	},
}
