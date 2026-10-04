package server

func InitRoutes(s *Server) {
	api := s.Group("/api")

	api.POST("/players/:id", s.handler.CreateOrUpdatePlayer)
	api.GET("/players/:id", s.handler.GetPlayer)
	api.PATCH("/players/:id/level", s.handler.UpdateLevel)
	api.POST("/players/:id/login", s.handler.Login)

	api.POST("/leaderboard/score", s.handler.AddScore)
	api.GET("/leaderboard/top", s.handler.TopPlayers)
	api.GET("/leaderboard/rank/:playerId", s.handler.PlayerRank)

	api.POST("/players/:id/achievements", s.handler.AddAchievement)
	api.GET("/players/:id/achievements/:name", s.handler.HasAchievement)
	api.GET("/players/:id1/achievements/common/:id2", s.handler.CommonAchievements)

	api.POST("/players/batch", s.handler.BatchCreatePlayers)
}
