package app

func list[T any](a *App, bucket string) []T {
	items, err := readList[T](a, bucket)
	if err != nil {
		panic(err)
	}
	return items
}
