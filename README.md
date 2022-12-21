![][visitors]
![][followers]
![][stars]

![](https://img.shields.io/badge/Platforms-Android_|_iOS_|_KMM_|_Web-blueviolet?logo=appveyor&style=?style=for-the-badge&logo=appveyor)

------

### Hi, I’m Rob 👋 - Head of Engineering @ [Cognetivity](https://cognetivity.com)

## 😀 ☀️ 🎉
```kotlin
sealed interface Interest {
  object Tech {
    object IOS: Tech
    object Android: Tech
    data class NativeMobile(val ios: IOS, val android: Android): Tech

    object Web: Tech
    object Server: Tech

    data class KMM(
      val coroutines: Coroutines,
      val native: NativeMobile,
      val web: Web,
      val server: Server
    ): Tech
  }

  object Quality {
    data class Architecture(val all: [Interests]): Quality
    object BestPractices: Quality
    object HexagonalArchitecture: Quality
    object TDD: Quality
    object Management: Quality
  }
}
```

## 🌱 📖 ✨
```kotlin
fun main() {
  val allInterests = Interest::class.sealedSubclasses
  learn(studies = allInterests)
}
```

## 📫 🔗 📨
[![Linkedin Badge](https://img.shields.io/badge/-LinkedIn-blue?style=flat-square&logo=Linkedin&logoColor=white)][linkedin]

<sub>(Sorry in advance if I don't reply, I can be a bit s*** at it. Mention github and I'll be more likely to connect!)</sub>

[visitors]: https://visitor-badge.laobi.icu/badge?page_id=RowbDowg-me
[followers]: https://img.shields.io/github/followers/RowbDowg
[stars]: https://img.shields.io/github/stars/RowbDowg
[linkedin]: https://www.linkedin.com/in/rob-valdes/
